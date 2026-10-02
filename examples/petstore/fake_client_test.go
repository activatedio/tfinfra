package petstore_test

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
)

// fakePetStoreClient implements petstorev1.PetStoreServiceClient over an
// in-memory map, returning gRPC NotFound statuses like a real server. It
// records the last request of each kind so tests can assert what the
// generated adapters sent.
type fakePetStoreClient struct {
	pets map[string]*petstorev1.Pet
	toys map[string]map[string]bool
	// toyEntities are the caller-named Toy resources, keyed by full name.
	toyEntities map[string]*petstorev1.Toy
	seq         int

	lastCreateParent string
	lastPatchPaths   []string
	lastListParent   string
	// repeatPageToken makes ListPets hand back the same page token forever,
	// the misbehaving API the list runtime must not loop on.
	repeatPageToken     bool
	lastAssociateSet    []string
	lastAssociateRemove []string
	// lastCreateToyID is the id the create request carried in the entity's
	// name field — what a caller-named API keys the row on.
	lastCreateToyID string
	// lastIntake records the input-only fields as they arrived, since the
	// stored row deliberately does not keep them.
	lastIntakeCode    string
	lastIntakeAgeDays int32
	// accessKeys are the minted rows, keyed by full name. Like a real mint
	// API they hold no key: it exists only in the mint response.
	accessKeys map[string]*petstorev1.AccessKey
	lastMint   *petstorev1.MintAccessKeyRequest
	// shelters and runs are the id-field resources, keyed by full name.
	shelters map[string]*petstorev1.Shelter
	runs     map[string]*petstorev1.Run
	// lastCreateShelter and lastCreateRun are the entities as the create
	// requests carried them, before the server filled anything in.
	lastCreateShelter *petstorev1.Shelter
	lastCreateRun     *petstorev1.Run
	// runsOmitShelterID makes run reads leave shelter_id empty, a server
	// that does not echo the parent's id it was given.
	runsOmitShelterID bool
	// writes counts pet writes, for update_time.
	writes int
	// breeds are the never-deleted records, keyed by full name.
	breeds map[string]*petstorev1.Breed
}

// consumeIntake mirrors a server that takes the input-only intake fields,
// acts on them, and never stores or returns them. Keeping them would make
// the generated read look correct for the wrong reason.
func (f *fakePetStoreClient) consumeIntake(p *petstorev1.Pet) {
	f.lastIntakeCode = p.GetIntakeCode()
	f.lastIntakeAgeDays = p.GetIntakeAgeDays()
	p.IntakeCode = ""
	p.IntakeAgeDays = 0
}

func newFakePetStoreClient() *fakePetStoreClient {
	return &fakePetStoreClient{
		pets:        map[string]*petstorev1.Pet{},
		toyEntities: map[string]*petstorev1.Toy{},
		accessKeys:  map[string]*petstorev1.AccessKey{},
		shelters:    map[string]*petstorev1.Shelter{},
		runs:        map[string]*petstorev1.Run{},
		breeds:      map[string]*petstorev1.Breed{},
	}
}

func (f *fakePetStoreClient) GetPet(_ context.Context, in *petstorev1.GetPetRequest, _ ...grpc.CallOption) (*petstorev1.Pet, error) {
	p, ok := f.pets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	return proto.Clone(p).(*petstorev1.Pet), nil
}

func (f *fakePetStoreClient) ListPets(_ context.Context, in *petstorev1.ListPetsRequest, _ ...grpc.CallOption) (*petstorev1.ListPetsResponse, error) {
	f.lastListParent = in.GetParent()

	// One pet per page, in name order, so a list has to follow its tokens.
	names := make([]string, 0, len(f.pets))
	for name := range f.pets {
		if strings.HasPrefix(name, in.GetParent()+"/pets/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	start := 0
	if in.GetPageToken() != "" {
		start, _ = strconv.Atoi(in.GetPageToken())
	}
	res := &petstorev1.ListPetsResponse{}
	if start < len(names) {
		res.Pets = []*petstorev1.Pet{proto.Clone(f.pets[names[start]]).(*petstorev1.Pet)}
		if start+1 < len(names) {
			res.NextPageToken = strconv.Itoa(start + 1)
		}
	}
	if f.repeatPageToken {
		res.NextPageToken = "again"
	}
	return res, nil
}

func (f *fakePetStoreClient) CreatePet(_ context.Context, in *petstorev1.CreatePetRequest, _ ...grpc.CallOption) (*petstorev1.Pet, error) {
	f.lastCreateParent = in.GetParent()
	f.seq++
	p := proto.Clone(in.GetPet()).(*petstorev1.Pet)
	p.Name = fmt.Sprintf("%s/pets/p%d", in.GetParent(), f.seq)
	p.CreateTime = timestamppb.New(createTimeFixture)
	f.touch(p)
	f.consumeIntake(p)
	f.pets[p.GetName()] = p
	return proto.Clone(p).(*petstorev1.Pet), nil
}

func (f *fakePetStoreClient) UpdatePet(_ context.Context, in *petstorev1.UpdatePetRequest, _ ...grpc.CallOption) (*petstorev1.Pet, error) {
	existing, ok := f.pets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	p := proto.Clone(in.GetPet()).(*petstorev1.Pet)
	p.Name = in.GetName()
	p.CreateTime = existing.GetCreateTime()
	f.touch(p)
	f.consumeIntake(p)
	f.pets[in.GetName()] = p
	return proto.Clone(p).(*petstorev1.Pet), nil
}

func (f *fakePetStoreClient) PatchPet(_ context.Context, in *petstorev1.PatchPetRequest, _ ...grpc.CallOption) (*petstorev1.Pet, error) {
	existing, ok := f.pets[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	f.lastPatchPaths = in.GetUpdateMask().GetPaths()
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetPet().GetDisplayName()
		case "type":
			existing.Type = in.GetPet().GetType()
		case "age":
			existing.Age = in.GetPet().GetAge()
		case "vaccinated":
			existing.Vaccinated = in.GetPet().GetVaccinated()
		case "weight":
			existing.Weight = in.GetPet().GetWeight()
		case "tags":
			existing.Tags = in.GetPet().GetTags()
		case "labels":
			existing.Labels = in.GetPet().GetLabels()
		case "config":
			existing.Config = in.GetPet().GetConfig()
		case "metadata":
			existing.Metadata = in.GetPet().GetMetadata()
		case "feeding":
			existing.Feeding = in.GetPet().GetFeeding()
		case "grooming_interval":
			existing.GroomingInterval = in.GetPet().GetGroomingInterval()
		case "vaccinations":
			existing.Vaccinations = in.GetPet().GetVaccinations()
		case "notes":
			existing.Notes = in.GetPet().GetNotes()
		case "intake_code", "intake_age_days":
			// Consumed, never stored: the row keeps no trace of them.
			f.lastIntakeCode = in.GetPet().GetIntakeCode()
			f.lastIntakeAgeDays = in.GetPet().GetIntakeAgeDays()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	f.touch(existing)
	return proto.Clone(existing).(*petstorev1.Pet), nil
}

// touch stamps a pet's update_time as a server does on every write: a
// minute later each time, so consecutive writes differ.
func (f *fakePetStoreClient) touch(p *petstorev1.Pet) {
	f.writes++
	p.UpdateTime = timestamppb.New(createTimeFixture.Add(time.Duration(f.writes) * time.Minute))
}

func (f *fakePetStoreClient) DeletePet(_ context.Context, in *petstorev1.DeletePetRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.pets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	delete(f.pets, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Toy: the caller-named lane. Create takes the row's id from the
// entity's name field and composes the full name from the parent, the way
// kit's name-keyed entities do.

func (f *fakePetStoreClient) GetToy(_ context.Context, in *petstorev1.GetToyRequest, _ ...grpc.CallOption) (*petstorev1.Toy, error) {
	t, ok := f.toyEntities[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "toy %q not found", in.GetName())
	}
	return proto.Clone(t).(*petstorev1.Toy), nil
}

func (f *fakePetStoreClient) ListToys(_ context.Context, _ *petstorev1.ListToysRequest, _ ...grpc.CallOption) (*petstorev1.ListToysResponse, error) {
	res := &petstorev1.ListToysResponse{}
	for _, t := range f.toyEntities {
		res.Toys = append(res.Toys, proto.Clone(t).(*petstorev1.Toy))
	}
	return res, nil
}

func (f *fakePetStoreClient) CreateToy(_ context.Context, in *petstorev1.CreateToyRequest, _ ...grpc.CallOption) (*petstorev1.Toy, error) {

	f.lastCreateParent = in.GetParent()
	f.lastCreateToyID = in.GetToy().GetName()

	if f.lastCreateToyID == "" {
		return nil, status.Error(codes.InvalidArgument, "toy name is required")
	}

	t := proto.Clone(in.GetToy()).(*petstorev1.Toy)
	t.Name = fmt.Sprintf("%s/toys/%s", in.GetParent(), f.lastCreateToyID)
	if _, exists := f.toyEntities[t.GetName()]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "toy %q already exists", t.GetName())
	}
	f.toyEntities[t.GetName()] = t

	return proto.Clone(t).(*petstorev1.Toy), nil
}

func (f *fakePetStoreClient) UpdateToy(_ context.Context, in *petstorev1.UpdateToyRequest, _ ...grpc.CallOption) (*petstorev1.Toy, error) {
	if _, ok := f.toyEntities[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "toy %q not found", in.GetName())
	}
	t := proto.Clone(in.GetToy()).(*petstorev1.Toy)
	t.Name = in.GetName()
	f.toyEntities[in.GetName()] = t
	return proto.Clone(t).(*petstorev1.Toy), nil
}

func (f *fakePetStoreClient) PatchToy(_ context.Context, in *petstorev1.PatchToyRequest, _ ...grpc.CallOption) (*petstorev1.Toy, error) {
	existing, ok := f.toyEntities[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "toy %q not found", in.GetName())
	}
	f.lastPatchPaths = in.GetUpdateMask().GetPaths()
	for _, path := range in.GetUpdateMask().GetPaths() {
		if path != "display_name" {
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
		existing.DisplayName = in.GetToy().GetDisplayName()
	}
	return proto.Clone(existing).(*petstorev1.Toy), nil
}

func (f *fakePetStoreClient) DeleteToy(_ context.Context, in *petstorev1.DeleteToyRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.toyEntities[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "toy %q not found", in.GetName())
	}
	delete(f.toyEntities, in.GetName())
	return &emptypb.Empty{}, nil
}

// AssociateToysToPet applies set/remove semantics over the pet's toy set
// and records the last edge payload so tests can assert what the generated
// adapter sent.
func (f *fakePetStoreClient) AssociateToysToPet(_ context.Context, in *petstorev1.AssociateToysToPetRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.pets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	f.lastAssociateSet = in.GetAssociation().GetSet()
	f.lastAssociateRemove = in.GetAssociation().GetRemove()
	if f.toys == nil {
		f.toys = map[string]map[string]bool{}
	}
	if f.toys[in.GetName()] == nil {
		f.toys[in.GetName()] = map[string]bool{}
	}
	for _, n := range in.GetAssociation().GetSet() {
		f.toys[in.GetName()][n] = true
	}
	for _, n := range in.GetAssociation().GetRemove() {
		delete(f.toys[in.GetName()], n)
	}
	return &emptypb.Empty{}, nil
}

// ListToysByPet pages one toy at a time so the runtime's token walk is
// exercised.
func (f *fakePetStoreClient) ListToysByPet(_ context.Context, in *petstorev1.ListToysByPetRequest, _ ...grpc.CallOption) (*petstorev1.ListToysByPetResponse, error) {
	if _, ok := f.pets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	names := f.petToys(in.GetName())
	start := 0
	if in.GetPageToken() != "" {
		fmt.Sscanf(in.GetPageToken(), "%d", &start)
	}
	res := &petstorev1.ListToysByPetResponse{}
	if start < len(names) {
		res.Toys = []*petstorev1.Toy{{Name: names[start]}}
		if start+1 < len(names) {
			res.NextPageToken = fmt.Sprintf("%d", start+1)
		}
	}
	return res, nil
}

// petToys returns the pet's current toy names, sorted.
func (f *fakePetStoreClient) petToys(pet string) []string {
	names := make([]string, 0, len(f.toys[pet]))
	for n := range f.toys[pet] {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (f *fakePetStoreClient) GetAccessKey(_ context.Context, in *petstorev1.GetAccessKeyRequest, _ ...grpc.CallOption) (*petstorev1.AccessKey, error) {
	k, ok := f.accessKeys[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access key %q not found", in.GetName())
	}
	return proto.Clone(k).(*petstorev1.AccessKey), nil
}

func (f *fakePetStoreClient) ListAccessKeys(_ context.Context, in *petstorev1.ListAccessKeysRequest, _ ...grpc.CallOption) (*petstorev1.ListAccessKeysResponse, error) {
	names := make([]string, 0, len(f.accessKeys))
	for name := range f.accessKeys {
		if strings.HasPrefix(name, in.GetParent()+"/accessKeys/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := &petstorev1.ListAccessKeysResponse{}
	for _, name := range names {
		out.AccessKeys = append(out.AccessKeys, proto.Clone(f.accessKeys[name]).(*petstorev1.AccessKey))
	}
	return out, nil
}

func (f *fakePetStoreClient) MintAccessKey(_ context.Context, in *petstorev1.MintAccessKeyRequest, _ ...grpc.CallOption) (*petstorev1.MintAccessKeyResponse, error) {
	f.lastMint = proto.Clone(in).(*petstorev1.MintAccessKeyRequest)
	f.seq++
	id := fmt.Sprintf("k%d", f.seq)
	k := &petstorev1.AccessKey{
		Name:        in.GetParent() + "/accessKeys/" + id,
		DisplayName: in.GetDisplayName(),
		ExpiresAt:   in.GetExpiresAt(),
		CreateTime:  timestamppb.New(createTimeFixture),
	}
	f.accessKeys[k.GetName()] = k
	return &petstorev1.MintAccessKeyResponse{
		AccessKey: proto.Clone(k).(*petstorev1.AccessKey),
		Key:       id + "_plaintext" + strconv.Itoa(f.seq),
	}, nil
}

func (f *fakePetStoreClient) PatchAccessKey(_ context.Context, in *petstorev1.PatchAccessKeyRequest, _ ...grpc.CallOption) (*petstorev1.AccessKey, error) {
	existing, ok := f.accessKeys[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "access key %q not found", in.GetName())
	}
	f.lastPatchPaths = in.GetUpdateMask().GetPaths()
	for _, path := range in.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			existing.DisplayName = in.GetAccessKey().GetDisplayName()
		case "expires_at":
			existing.ExpiresAt = in.GetAccessKey().GetExpiresAt()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*petstorev1.AccessKey), nil
}

func (f *fakePetStoreClient) DeleteAccessKey(_ context.Context, in *petstorev1.DeleteAccessKeyRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.accessKeys[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "access key %q not found", in.GetName())
	}
	delete(f.accessKeys, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Shelter and Run: the id-field lane. Create takes the row's id from
// a field of the entity and ignores name, the way riteSuite's estate
// records do; the path is authoritative on update, and there is no Patch.

func (f *fakePetStoreClient) GetShelter(_ context.Context, in *petstorev1.GetShelterRequest, _ ...grpc.CallOption) (*petstorev1.Shelter, error) {
	s, ok := f.shelters[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "shelter %q not found", in.GetName())
	}
	return proto.Clone(s).(*petstorev1.Shelter), nil
}

func (f *fakePetStoreClient) ListShelters(_ context.Context, _ *petstorev1.ListSheltersRequest, _ ...grpc.CallOption) (*petstorev1.ListSheltersResponse, error) {
	names := make([]string, 0, len(f.shelters))
	for name := range f.shelters {
		names = append(names, name)
	}
	sort.Strings(names)
	res := &petstorev1.ListSheltersResponse{}
	for _, name := range names {
		res.Shelters = append(res.Shelters, proto.Clone(f.shelters[name]).(*petstorev1.Shelter))
	}
	return res, nil
}

func (f *fakePetStoreClient) CreateShelter(_ context.Context, in *petstorev1.CreateShelterRequest, _ ...grpc.CallOption) (*petstorev1.Shelter, error) {
	f.lastCreateParent = in.GetParent()
	f.lastCreateShelter = proto.Clone(in.GetShelter()).(*petstorev1.Shelter)
	if in.GetShelter().GetShelterId() == "" {
		return nil, status.Error(codes.InvalidArgument, "shelter_id is required")
	}
	s := proto.Clone(in.GetShelter()).(*petstorev1.Shelter)
	s.Name = "shelters/" + s.GetShelterId()
	if _, exists := f.shelters[s.GetName()]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "shelter %q already exists", s.GetName())
	}
	f.shelters[s.GetName()] = s
	return proto.Clone(s).(*petstorev1.Shelter), nil
}

func (f *fakePetStoreClient) UpdateShelter(_ context.Context, in *petstorev1.UpdateShelterRequest, _ ...grpc.CallOption) (*petstorev1.Shelter, error) {
	existing, ok := f.shelters[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "shelter %q not found", in.GetName())
	}
	s := proto.Clone(in.GetShelter()).(*petstorev1.Shelter)
	s.Name = existing.GetName()
	s.ShelterId = existing.GetShelterId()
	f.shelters[s.GetName()] = s
	return proto.Clone(s).(*petstorev1.Shelter), nil
}

func (f *fakePetStoreClient) DeleteShelter(_ context.Context, in *petstorev1.DeleteShelterRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.shelters[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "shelter %q not found", in.GetName())
	}
	delete(f.shelters, in.GetName())
	return &emptypb.Empty{}, nil
}

// readRun is what a run read returns: the stored row, without shelter_id
// when the fake is set not to echo it.
func (f *fakePetStoreClient) readRun(r *petstorev1.Run) *petstorev1.Run {
	out := proto.Clone(r).(*petstorev1.Run)
	if f.runsOmitShelterID {
		out.ShelterId = ""
	}
	return out
}

func (f *fakePetStoreClient) GetRun(_ context.Context, in *petstorev1.GetRunRequest, _ ...grpc.CallOption) (*petstorev1.Run, error) {
	r, ok := f.runs[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "run %q not found", in.GetName())
	}
	return f.readRun(r), nil
}

func (f *fakePetStoreClient) ListRuns(_ context.Context, in *petstorev1.ListRunsRequest, _ ...grpc.CallOption) (*petstorev1.ListRunsResponse, error) {
	f.lastListParent = in.GetParent()
	names := make([]string, 0, len(f.runs))
	for name := range f.runs {
		if strings.HasPrefix(name, in.GetParent()+"/runs/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	res := &petstorev1.ListRunsResponse{}
	for _, name := range names {
		res.Runs = append(res.Runs, f.readRun(f.runs[name]))
	}
	return res, nil
}

// CreateRun takes the parent's id from the parent, whatever the entity
// says, and mints a run_id when the entity leaves it empty.
func (f *fakePetStoreClient) CreateRun(_ context.Context, in *petstorev1.CreateRunRequest, _ ...grpc.CallOption) (*petstorev1.Run, error) {
	f.lastCreateParent = in.GetParent()
	f.lastCreateRun = proto.Clone(in.GetRun()).(*petstorev1.Run)
	shelter, ok := strings.CutPrefix(in.GetParent(), "shelters/")
	if !ok || shelter == "" {
		return nil, status.Errorf(codes.InvalidArgument, "parent %q is not a shelter", in.GetParent())
	}
	r := proto.Clone(in.GetRun()).(*petstorev1.Run)
	if r.GetRunId() == "" {
		f.seq++
		r.RunId = fmt.Sprintf("run%d", f.seq)
	}
	r.ShelterId = shelter
	r.Name = in.GetParent() + "/runs/" + r.GetRunId()
	if _, exists := f.runs[r.GetName()]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "run %q already exists", r.GetName())
	}
	f.runs[r.GetName()] = r
	return f.readRun(r), nil
}

func (f *fakePetStoreClient) UpdateRun(_ context.Context, in *petstorev1.UpdateRunRequest, _ ...grpc.CallOption) (*petstorev1.Run, error) {
	existing, ok := f.runs[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "run %q not found", in.GetName())
	}
	r := proto.Clone(in.GetRun()).(*petstorev1.Run)
	r.Name = existing.GetName()
	r.RunId = existing.GetRunId()
	r.ShelterId = existing.GetShelterId()
	f.runs[r.GetName()] = r
	return f.readRun(r), nil
}

func (f *fakePetStoreClient) DeleteRun(_ context.Context, in *petstorev1.DeleteRunRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.runs[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "run %q not found", in.GetName())
	}
	delete(f.runs, in.GetName())
	return &emptypb.Empty{}, nil
}

// --- Breed: no Delete.

func (f *fakePetStoreClient) GetBreed(_ context.Context, in *petstorev1.GetBreedRequest, _ ...grpc.CallOption) (*petstorev1.Breed, error) {
	b, ok := f.breeds[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "breed %q not found", in.GetName())
	}
	return proto.Clone(b).(*petstorev1.Breed), nil
}

func (f *fakePetStoreClient) ListBreeds(_ context.Context, _ *petstorev1.ListBreedsRequest, _ ...grpc.CallOption) (*petstorev1.ListBreedsResponse, error) {
	res := &petstorev1.ListBreedsResponse{}
	for _, b := range f.breeds {
		res.Breeds = append(res.Breeds, proto.Clone(b).(*petstorev1.Breed))
	}
	return res, nil
}

func (f *fakePetStoreClient) CreateBreed(_ context.Context, in *petstorev1.CreateBreedRequest, _ ...grpc.CallOption) (*petstorev1.Breed, error) {
	b := proto.Clone(in.GetBreed()).(*petstorev1.Breed)
	b.Name = "breeds/" + b.GetBreedId()
	f.breeds[b.GetName()] = b
	return proto.Clone(b).(*petstorev1.Breed), nil
}

func (f *fakePetStoreClient) UpdateBreed(_ context.Context, in *petstorev1.UpdateBreedRequest, _ ...grpc.CallOption) (*petstorev1.Breed, error) {
	existing, ok := f.breeds[in.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "breed %q not found", in.GetName())
	}
	b := proto.Clone(in.GetBreed()).(*petstorev1.Breed)
	b.Name, b.BreedId = existing.GetName(), existing.GetBreedId()
	f.breeds[b.GetName()] = b
	return proto.Clone(b).(*petstorev1.Breed), nil
}
