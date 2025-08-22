package instancetype

import (
	"context"

	egov3 "github.com/exoscale/egoscale/v3"
)

type ExoscaleClient interface {
	ListInstanceTypes(ctx context.Context) (*egov3.ListInstanceTypesResponse, error)
}
