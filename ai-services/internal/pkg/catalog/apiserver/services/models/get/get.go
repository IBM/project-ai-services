// Package get implements the model detail operation.
package get

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// Getter builds the detail of one model.
type Getter struct {
	svcDepRepo   dbrepo.ServiceDependencyRepository
	modelCatalog helpers.ModelCatalog
	// gatewayPublicEndpoint is the URL consumers call models at, reported as the model's endpoint.
	// Empty when it is not configured.
	gatewayPublicEndpoint string
}

// New creates a Getter. gatewayPublicEndpoint is the public URL of the model gateway, reported as
// the endpoint of every model; it may be empty.
func New(
	svcDepRepo dbrepo.ServiceDependencyRepository,
	catalogProvider *catalog.CatalogProvider,
	gatewayPublicEndpoint string,
) *Getter {
	return &Getter{
		svcDepRepo:            svcDepRepo,
		modelCatalog:          helpers.New(catalogProvider),
		gatewayPublicEndpoint: gatewayPublicEndpoint,
	}
}

// Detail returns the detail of a model, local or remote: the applications using it, the number of
// services using it and its endpoint.
//
// The row must be one the caller may see: Detail does not check ownership.
func (g *Getter) Detail(ctx context.Context, row *dbrepo.ModelRow) (*apimodels.ModelDetail, error) {
	depType := helpers.ModelDependencyType(*row)

	applications, err := g.applications(ctx, row.ID, depType)
	if err != nil {
		return nil, err
	}

	counts, err := g.svcDepRepo.GetServiceCountByDependency(ctx, []uuid.UUID{row.ID}, depType)
	if err != nil {
		return nil, fmt.Errorf("failed to count services using model: %w", err)
	}

	detail := &apimodels.ModelDetail{
		ModelBase:    helpers.ToModelBase(*row, g.modelCatalog.ProviderNames(), counts[row.ID]),
		Applications: applications,
		Endpoints:    g.endpoints(),
	}

	return detail, nil
}

// endpoints returns the endpoint through which a model, local or remote, is called: the public
// model gateway.
// The url is the configured gateway public endpoint exactly as given, empty when it is not set.
func (g *Getter) endpoints() []apimodels.ModelEndpoint {
	return []apimodels.ModelEndpoint{{
		Type: constants.ModelEndpointTypeExternal,
		URL:  g.gatewayPublicEndpoint,
	}}
}

// applications returns the applications that use a model, one entry per application, sorted
// by name.
func (g *Getter) applications(ctx context.Context, id uuid.UUID, depType dbmodels.DependencyType) ([]apimodels.ModelApplication, error) {
	linked, err := g.svcDepRepo.GetLinkedServiceEndpoints(ctx, id, depType)
	if err != nil {
		return nil, fmt.Errorf("failed to get applications using model: %w", err)
	}

	// Several services of one application can use the same model: list the application once.
	seen := make(map[uuid.UUID]bool, len(linked))
	applications := make([]apimodels.ModelApplication, 0, len(linked))

	for _, l := range linked {
		if seen[l.ApplicationID] {
			continue
		}

		seen[l.ApplicationID] = true
		applications = append(applications, apimodels.ModelApplication{ID: l.ApplicationID.String(), Name: l.ApplicationName})
	}

	slices.SortFunc(applications, func(a, b apimodels.ModelApplication) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})

	return applications, nil
}
