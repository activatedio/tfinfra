package tf

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"
)

// ProviderData is the contract between a provider's Configure and the
// generated resources and data sources: the provider places a *ProviderData
// in resp.ResourceData / resp.DataSourceData.
//
// Clients is keyed by the spec's Resource.Client key; Defaults carries the
// provider-level scope identifier defaults (e.g. "tenant_id") that
// per-resource attributes override.
type ProviderData struct {
	Clients  map[string]any
	Defaults map[string]string
}

// Model is implemented by generated plan/state models: proto conversions
// plus the accessors the Crud runtime needs. M is the implementing pointer
// type itself (curiously recurring), so UpdateMask stays fully typed.
type Model[E proto.Message, M any] interface {
	ToProto(ctx context.Context) (E, diag.Diagnostics)
	FromProto(ctx context.Context, e E) diag.Diagnostics
	// GetName returns the AIP resource name attribute (the Terraform ID).
	GetName() types.String
	// ScopeIdentifiers returns the per-resource scope attribute values
	// (null attributes as ""), keyed by attribute name.
	ScopeIdentifiers() map[string]string
	// UpdateMask returns the proto field paths whose values differ from
	// prior, skipping computed fields and scope identifiers. JSON-typed
	// attributes compare semantically.
	UpdateMask(ctx context.Context, prior M) []string
}

// CrudClient adapts one resource's operations on a gRPC client. Generated
// code wires each func to the corresponding stub call; unused operations
// stay nil and the runtime reports a clear diagnostic if one is exercised.
type CrudClient[E proto.Message] struct {
	Get    func(ctx context.Context, name string) (E, error)
	List   func(ctx context.Context, parent string, pageToken string) (items []E, nextPageToken string, err error)
	Create func(ctx context.Context, parent string, entity E) (E, error)
	Update func(ctx context.Context, name string, entity E) (E, error)
	Patch  func(ctx context.Context, name string, entity E, mask []string) (E, error)
	Delete func(ctx context.Context, name string) error
}

// CrudParams configures a Crud runtime instance.
type CrudParams[E proto.Message, M Model[E, M]] struct {
	// TypeName is the resource's Terraform type suffix, e.g. "pet" for
	// <provider>_pet.
	TypeName string
	// Collection is the resource's own AIP collection, e.g. "pets".
	Collection string
	Scope      Scope
	// Defaults are the provider-level scope identifier defaults.
	Defaults map[string]string
	NewModel func() M
	Client   CrudClient[E]
	// UseUpdate selects the full-replace Update operation instead of
	// Patch with an update mask.
	UseUpdate bool
}

// Crud is the generic runtime behind generated resources and singular data
// sources: generated code stays thin glue over it.
type Crud[E proto.Message, M Model[E, M]] struct {
	params CrudParams[E, M]
}

// NewCrud creates the runtime for one resource.
func NewCrud[E proto.Message, M Model[E, M]](params CrudParams[E, M]) *Crud[E, M] {
	return &Crud[E, M]{params: params}
}

// resolveParent merges the model's scope identifiers over the provider
// defaults and composes the AIP parent.
func (c *Crud[E, M]) resolveParent(m M) (string, error) {

	merged := map[string]string{}

	for _, attr := range c.params.Scope.IdentifierAttributes() {
		merged[attr] = c.params.Defaults[attr]
	}
	for attr, v := range m.ScopeIdentifiers() {
		if v != "" {
			merged[attr] = v
		}
	}

	return c.params.Scope.ComposeParent(merged)
}

// Create implements resource.Resource Create.
func (c *Crud[E, M]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if c.params.Client.Create == nil {
		resp.Diagnostics.AddError("operation not supported", fmt.Sprintf("%s does not support create", c.params.TypeName))
		return
	}

	m := c.params.NewModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parent, err := c.resolveParent(m)
	if err != nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("cannot resolve parent for %s", c.params.TypeName),
			err.Error()+"; set it on the resource or as a provider default",
		)
		return
	}

	e, diags := m.ToProto(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := c.params.Client.Create(ctx, parent, e)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("create %s failed", c.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(m.FromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// Read implements resource.Resource Read. A gRPC NotFound removes the
// resource from state so the next plan recreates it.
func (c *Crud[E, M]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	m := c.params.NewModel()
	resp.Diagnostics.Append(req.State.Get(ctx, m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := c.params.Client.Get(ctx, m.GetName().ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(fmt.Sprintf("read %s failed", c.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(m.FromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// Update implements resource.Resource Update: Patch with an update mask
// computed from the plan/state diff, or full-replace Update when configured.
func (c *Crud[E, M]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	plan := c.params.NewModel()
	state := c.params.NewModel()
	resp.Diagnostics.Append(req.Plan.Get(ctx, plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.GetName().ValueString()

	e, diags := plan.ToProto(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := c.doUpdate(ctx, name, e, plan, state)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("update %s failed", c.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(plan.FromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (c *Crud[E, M]) doUpdate(ctx context.Context, name string, e E, plan, state M) (E, error) {

	if c.params.UseUpdate || c.params.Client.Patch == nil {
		if c.params.Client.Update == nil {
			var zero E
			return zero, fmt.Errorf("%s supports neither patch nor update", c.params.TypeName)
		}
		return c.params.Client.Update(ctx, name, e)
	}

	mask := plan.UpdateMask(ctx, state)
	if len(mask) == 0 {
		// Nothing diffable changed; read back the current entity instead of
		// issuing an empty patch.
		return c.params.Client.Get(ctx, name)
	}

	return c.params.Client.Patch(ctx, name, e, mask)
}

// Delete implements resource.Resource Delete. NotFound counts as success:
// the resource is already gone.
func (c *Crud[E, M]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if c.params.Client.Delete == nil {
		resp.Diagnostics.AddError("operation not supported", fmt.Sprintf("%s does not support delete", c.params.TypeName))
		return
	}

	m := c.params.NewModel()
	resp.Diagnostics.Append(req.State.Get(ctx, m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := c.params.Client.Delete(ctx, m.GetName().ValueString())
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("delete %s failed", c.params.TypeName), err.Error())
	}
}

// ImportState implements resource.ResourceWithImportState: the import ID is
// the full AIP resource name.
func (c *Crud[E, M]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	if _, _, err := c.params.Scope.ParseName(c.params.Collection, req.ID); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("invalid import ID for %s", c.params.TypeName), err.Error())
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root(NameAttribute), req, resp)
}

// ReadDataSource implements the singular data source Read: Get by full name.
// Unlike resource Read, NotFound is an error.
func (c *Crud[E, M]) ReadDataSource(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	m := c.params.NewModel()
	resp.Diagnostics.Append(req.Config.Get(ctx, m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := c.params.Client.Get(ctx, m.GetName().ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("read %s failed", c.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(m.FromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// NameAttribute is the Terraform attribute holding the AIP resource name.
const NameAttribute = "name"
