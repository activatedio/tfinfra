package petstore_test

import (
	"context"
	"fmt"

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
	seq  int

	lastCreateParent string
	lastPatchPaths   []string
	lastListParent   string
}

func newFakePetStoreClient() *fakePetStoreClient {
	return &fakePetStoreClient{pets: map[string]*petstorev1.Pet{}}
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
	res := &petstorev1.ListPetsResponse{}
	for _, p := range f.pets {
		res.Pets = append(res.Pets, proto.Clone(p).(*petstorev1.Pet))
	}
	return res, nil
}

func (f *fakePetStoreClient) CreatePet(_ context.Context, in *petstorev1.CreatePetRequest, _ ...grpc.CallOption) (*petstorev1.Pet, error) {
	f.lastCreateParent = in.GetParent()
	f.seq++
	p := proto.Clone(in.GetPet()).(*petstorev1.Pet)
	p.Name = fmt.Sprintf("%s/pets/p%d", in.GetParent(), f.seq)
	p.CreateTime = timestamppb.New(createTimeFixture)
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
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask path %q", path)
		}
	}
	return proto.Clone(existing).(*petstorev1.Pet), nil
}

func (f *fakePetStoreClient) DeletePet(_ context.Context, in *petstorev1.DeletePetRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	if _, ok := f.pets[in.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "pet %q not found", in.GetName())
	}
	delete(f.pets, in.GetName())
	return &emptypb.Empty{}, nil
}
