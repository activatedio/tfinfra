package tf

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsNotFound reports whether the error is a gRPC NotFound status. The Crud
// runtime uses it to translate out-of-band deletion into state removal on
// Read and into success on Delete.
func IsNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
