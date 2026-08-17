package tf

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// AssociationClient adapts one association edge's RPC pair on a gRPC
// client. Generated code wires each func to the corresponding stub call.
type AssociationClient struct {
	// Associate issues the Associate{Targets}To{Entity} call with the given
	// set and remove member names.
	Associate func(ctx context.Context, name string, set, remove []string) error
	// ListBy issues one List{Targets}By{Entity} page and returns the member
	// names on it; the runtime walks next-page tokens.
	ListBy func(ctx context.Context, name string, pageToken string) (names []string, nextPageToken string, err error)
}

// AssociationParams configures an Association runtime instance.
type AssociationParams struct {
	// TypeName is the resource's Terraform type suffix, e.g. "pet_toys".
	TypeName string
	// EntityAttribute is the attribute holding the entity's full resource
	// name, e.g. "pet".
	EntityAttribute string
	// Attribute is the attribute holding the member set, e.g. "toys".
	Attribute string
	// Scope and Collection identify the entity's position in the AIP
	// hierarchy; import IDs are validated against them.
	Scope      Scope
	Collection string
	Client     AssociationClient
}

// Association is the generic runtime behind generated association
// resources: the Terraform surface of the kit Associate{Targets}To{Entity}
// / List{Targets}By{Entity} RPC family.
//
// The resource is authoritative over the entity's full association set:
// members present on the server but absent from the configuration are
// removed on apply, and Delete removes every member.
type Association struct {
	params AssociationParams
}

// NewAssociation creates the runtime for one association edge.
func NewAssociation(params AssociationParams) *Association {
	return &Association{params: params}
}

// AssociationSchema builds the fixed two-attribute schema of an association
// resource: the entity's full resource name plus the authoritative member
// set.
func AssociationSchema(entityAttribute, attribute string) schema.Schema {

	human := func(s string) string { return strings.ReplaceAll(s, "_", " ") }

	return schema.Schema{
		MarkdownDescription: fmt.Sprintf(
			"Manages the full set of %s associated with one %s. The set is authoritative: members associated out of band are removed on the next apply.",
			human(attribute), human(entityAttribute)),
		Attributes: map[string]schema.Attribute{
			entityAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: fmt.Sprintf("Full resource name of the %s.", human(entityAttribute)),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			attribute: schema.SetAttribute{
				ElementType:         types.StringType,
				Required:            true,
				MarkdownDescription: fmt.Sprintf("Full resource names of the associated %s.", human(attribute)),
			},
		},
	}
}

// attrGetter is the shared shape of tfsdk.Plan and tfsdk.State.
type attrGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// read extracts the entity name and desired member set from a plan or
// state.
func (a *Association) read(ctx context.Context, g attrGetter) (string, []string, diag.Diagnostics) {

	var diags diag.Diagnostics

	var name types.String
	diags.Append(g.GetAttribute(ctx, path.Root(a.params.EntityAttribute), &name)...)

	var set types.Set
	diags.Append(g.GetAttribute(ctx, path.Root(a.params.Attribute), &set)...)
	if diags.HasError() {
		return "", nil, diags
	}

	var members []string
	if !set.IsNull() && !set.IsUnknown() {
		diags.Append(set.ElementsAs(ctx, &members, false)...)
	}

	return name.ValueString(), members, diags
}

// listAll walks the ListBy pages and returns the entity's current member
// names.
func (a *Association) listAll(ctx context.Context, name string) ([]string, error) {

	var out []string
	token := ""

	for {
		names, next, err := a.params.Client.ListBy(ctx, name, token)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
		if next == "" {
			return out, nil
		}
		token = next
	}
}

// difference returns the members of xs absent from ys, deduplicated and
// sorted for deterministic requests.
func difference(xs, ys []string) []string {

	drop := make(map[string]struct{}, len(ys))
	for _, y := range ys {
		drop[y] = struct{}{}
	}

	seen := map[string]struct{}{}
	out := []string{}
	for _, x := range xs {
		if _, ok := drop[x]; ok {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}

	sort.Strings(out)
	return out
}

// reconcile drives the server's association set to exactly desired.
func (a *Association) reconcile(ctx context.Context, name string, desired []string) error {

	current, err := a.listAll(ctx, name)
	if err != nil {
		return err
	}

	set := difference(desired, current)
	remove := difference(current, desired)
	if len(set) == 0 && len(remove) == 0 {
		return nil
	}

	return a.params.Client.Associate(ctx, name, set, remove)
}

// Create implements resource.Resource Create: it reconciles the server to
// the planned set, removing any members associated out of band.
func (a *Association) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	name, desired, diags := a.read(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := a.reconcile(ctx, name, desired); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("create %s failed", a.params.TypeName), err.Error())
		return
	}

	resp.State.Raw = req.Plan.Raw
}

// Read implements resource.Resource Read: the state set becomes the
// server's current membership. A gRPC NotFound on the entity removes the
// resource from state.
func (a *Association) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	var name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(a.params.EntityAttribute), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}

	current, err := a.listAll(ctx, name.ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(fmt.Sprintf("read %s failed", a.params.TypeName), err.Error())
		return
	}
	if current == nil {
		current = []string{}
	}

	resp.State.Raw = req.State.Raw
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(a.params.Attribute), current)...)
}

// Update implements resource.Resource Update: the same authoritative
// reconcile as Create against the planned set.
func (a *Association) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	name, desired, diags := a.read(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := a.reconcile(ctx, name, desired); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("update %s failed", a.params.TypeName), err.Error())
		return
	}

	resp.State.Raw = req.Plan.Raw
}

// Delete implements resource.Resource Delete: it removes every member
// currently associated on the server. A NotFound entity counts as success.
func (a *Association) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	var name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(a.params.EntityAttribute), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}

	current, err := a.listAll(ctx, name.ValueString())
	if err != nil {
		if IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(fmt.Sprintf("delete %s failed", a.params.TypeName), err.Error())
		return
	}
	if len(current) == 0 {
		return
	}

	if err := a.params.Client.Associate(ctx, name.ValueString(), nil, current); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("delete %s failed", a.params.TypeName), err.Error())
	}
}

// ImportState implements resource.ResourceWithImportState: the import ID is
// the entity's full AIP resource name; the member set fills in on the
// following Read.
func (a *Association) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := a.params.Scope.ParseName(a.params.Collection, req.ID); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("invalid import ID for %s", a.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(a.params.EntityAttribute), req.ID)...)
}
