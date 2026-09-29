package tf

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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
	// Mint stands in for Create on a resource the API mints: it returns the
	// entity plus the values the API hands over exactly once, keyed by the
	// attribute that holds each. Create uses it whenever it is set.
	Mint func(ctx context.Context, parent string, entity E) (E, map[string]string, error)
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
	// IDAttribute is the attribute holding the resource's own id, the last
	// segment of its name ("toy_id"). Every create, read, update, import and
	// data source read fills it from the name; empty leaves it out.
	IDAttribute string
	// CallerNamed marks a resource whose id the caller assigns: Create copies
	// IDAttribute's value into the entity's name field, where the server
	// takes it from.
	CallerNamed bool
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
	return c.composeParent(m.ScopeIdentifiers())
}

// composeParent composes the AIP parent from scope identifier values,
// falling back to the provider defaults for any left empty.
func (c *Crud[E, M]) composeParent(ids map[string]string) (string, error) {

	merged := map[string]string{}

	for _, attr := range c.params.Scope.IdentifierAttributes() {
		merged[attr] = c.params.Defaults[attr]
	}
	for attr, v := range ids {
		if v != "" {
			merged[attr] = v
		}
	}

	return c.params.Scope.ComposeParent(merged)
}

// ListAll returns every entity under the parent that ids compose (over the
// provider defaults), following page tokens until the API returns none. A
// page token the API has already returned ends the walk with an error
// rather than looping.
func (c *Crud[E, M]) ListAll(ctx context.Context, ids map[string]string) ([]E, error) {

	if c.params.Client.List == nil {
		return nil, fmt.Errorf("%s does not support list", c.params.TypeName)
	}

	parent, err := c.composeParent(ids)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve parent for %s: %w; set it on the data source or as a provider default", c.params.TypeName, err)
	}

	var all []E
	seen := map[string]bool{}
	token := ""
	for {
		page, next, err := c.params.Client.List(ctx, parent, token)
		if err != nil {
			return nil, fmt.Errorf("list %s under %s: %w", c.params.TypeName, parent, err)
		}
		all = append(all, page...)
		if next == "" {
			return all, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("list %s under %s: the API returned page token %q twice", c.params.TypeName, parent, next)
		}
		seen[next] = true
		token = next
	}
}

// IDFromName returns the last segment of a full resource name: the
// entity's own id.
func (c *Crud[E, M]) IDFromName(name string) (string, error) {
	_, id, err := c.params.Scope.ParseName(c.params.Collection, name)
	return id, err
}

// Create implements resource.Resource Create.
func (c *Crud[E, M]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if c.params.Client.Create == nil && c.params.Client.Mint == nil {
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

	resp.Diagnostics.Append(c.applyCallerID(ctx, req.Plan, e)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, once, err := c.doCreate(ctx, parent, e)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("create %s failed", c.params.TypeName), err.Error())
		return
	}

	resp.Diagnostics.Append(m.FromProto(ctx, out)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(c.storeID(ctx, &resp.State, m.GetName().ValueString())...)
	resp.Diagnostics.Append(storeOnce(ctx, &resp.State, once)...)
}

// applyCallerID copies a caller-named resource's id from the plan into the
// entity's name field, where the server takes it from and composes the
// full resource name with the parent. It does nothing for a server-named
// resource.
func (c *Crud[E, M]) applyCallerID(ctx context.Context, plan tfsdk.Plan, e E) diag.Diagnostics {

	if !c.params.CallerNamed {
		return nil
	}

	var id types.String
	diags := plan.GetAttribute(ctx, path.Root(c.params.IDAttribute), &id)
	if diags.HasError() {
		return diags
	}
	if err := setResourceID(e, id.ValueString()); err != nil {
		diags.AddAttributeError(path.Root(c.params.IDAttribute),
			fmt.Sprintf("cannot set the %s id", c.params.TypeName), err.Error())
	}

	return diags
}

// storeID writes the resource's own id, the last segment of name, into
// state. It does nothing for a resource with no id attribute.
func (c *Crud[E, M]) storeID(ctx context.Context, state *tfsdk.State, name string) diag.Diagnostics {

	var diags diag.Diagnostics
	if c.params.IDAttribute == "" {
		return diags
	}

	id, err := c.IDFromName(name)
	if err != nil {
		diags.AddError(fmt.Sprintf("unexpected %s name", c.params.TypeName), err.Error())
		return diags
	}

	return state.SetAttribute(ctx, path.Root(c.params.IDAttribute), id)
}

// storeOnce writes a mint's once-only values into state. No read returns
// them, so state is the only place they will ever be: FromProto cannot
// fill them and nothing after this touches them, so every refresh carries
// them forward.
func storeOnce(ctx context.Context, state *tfsdk.State, once map[string]string) diag.Diagnostics {

	var diags diag.Diagnostics
	for attr, v := range once {
		diags.Append(state.SetAttribute(ctx, path.Root(attr), types.StringValue(v))...)
	}

	return diags
}

// doCreate creates through Mint when the resource has one, else Create.
func (c *Crud[E, M]) doCreate(ctx context.Context, parent string, e E) (E, map[string]string, error) {

	if c.params.Client.Mint != nil {
		return c.params.Client.Mint(ctx, parent, e)
	}

	out, err := c.params.Client.Create(ctx, parent, e)
	return out, nil, err
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
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(c.storeID(ctx, &resp.State, m.GetName().ValueString())...)
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
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(c.storeID(ctx, &resp.State, plan.GetName().ValueString())...)
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

	_, id, err := c.params.Scope.ParseName(c.params.Collection, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("invalid import ID for %s", c.params.TypeName), err.Error())
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root(NameAttribute), req, resp)

	// The id lands in state on import: a caller-named resource's is required,
	// so without it the first plan would force replacement, and a server-named
	// one's is what other resources reference.
	if c.params.IDAttribute != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(c.params.IDAttribute), id)...)
	}
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
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(c.storeID(ctx, &resp.State, m.GetName().ValueString())...)
}

// NameAttribute is the Terraform attribute holding the AIP resource name.
const NameAttribute = "name"

// KeepersAttribute is the map a minted resource carries whose change
// replaces it: the rotation trigger for a credential with no rotate verb.
const KeepersAttribute = "keepers"

// setResourceID writes the caller-assigned id into the entity's AIP name
// field, which is where the API takes it from on create.
func setResourceID(e proto.Message, id string) error {

	if id == "" {
		return errors.New("the id must not be empty")
	}

	m := e.ProtoReflect()
	fd := m.Descriptor().Fields().ByName(NameAttribute)
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return fmt.Errorf("%s has no string %q field", m.Descriptor().FullName(), NameAttribute)
	}

	m.Set(fd, protoreflect.ValueOfString(id))

	return nil
}
