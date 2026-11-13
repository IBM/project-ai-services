package helm

import (
	"fmt"

	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
)

// MarshalChart extracts the raw files from a loaded *chartv2.Chart for gRPC
// wire transport. The returned files mirror Chart.Raw as stored by the loader.
func MarshalChart(chart helmchart.Charter) ([]*archive.BufferedFile, error) {
	c, ok := chart.(*chartv2.Chart)
	if !ok {
		return nil, fmt.Errorf("helm: unsupported chart type %T for remote install", chart)
	}

	files := make([]*archive.BufferedFile, len(c.Raw))
	for i, f := range c.Raw {
		files[i] = &archive.BufferedFile{Name: f.Name, Data: f.Data}
	}

	return files, nil
}

// UnmarshalChart reconstructs a *chartv2.Chart from payload.ChartFile wire
// types received over the gRPC CommandStream.
func UnmarshalChart(chartFiles []payload.ChartFile) (helmchart.Charter, error) {
	buffered := make([]*archive.BufferedFile, len(chartFiles))
	for i, f := range chartFiles {
		buffered[i] = &archive.BufferedFile{Name: f.Name, Data: f.Data}
	}

	ch, err := loader.LoadFiles(buffered)
	if err != nil {
		return nil, fmt.Errorf("helm: reconstruct chart: %w", err)
	}

	return ch, nil
}
