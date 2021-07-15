package fetch

import (
	"github.com/caos/orbos/internal/operator/boom/application"
	"github.com/caos/orbos/internal/operator/boom/bundle/bundles"
	"github.com/caos/orbos/internal/operator/boom/templator/helm/chart"
	"github.com/caos/orbos/internal/operator/boom/templator/helm/helmcommand"
	"github.com/caos/orbos/mntr"
)

type ChartKey struct {
	Name    string
	Version string
}

type ChartInfo struct {
	Name      string
	Version   string
	IndexName string
}

func All(monitor mntr.Monitor, basePath string, newVersions bool) error {
	allApps := bundles.GetAll()

	monitor.Info("Ingest Helm")

	// helm init to create a HELMHOME
	if err := helmcommand.Init(basePath); err != nil {
		return err
	}

	//indexes in a map so that no doublicates exist
	indexes := make(map[*ChartKey]*chart.Index, 0)
	charts := make([]*ChartInfo, 0)
	monitor.Info("Preparing lists of indexes and charts")

	for _, appName := range allApps {
		app := application.New(monitor, appName, "")
		temp, ok := app.(application.HelmApplication)
		// if application doenst implement helm interface then no charts are defined
		if ok {
			// get chartinfo from application
			chart := temp.GetChartInfo()

			// when no index defined then it the helm stable repository
			var indexName string
			if chart.Index != nil {
				indexName = chart.Index.Name
				indexes[&ChartKey{Name: chart.Name, Version: chart.Version}] = chart.Index
			} else {
				indexName = "stable"
			}

			// only add chart if chart is not used by another application, no doublicates
			var found bool
			found = false
			for _, checkChart := range charts {
				if checkChart.Name == chart.Name && checkChart.Version == chart.Version && checkChart.IndexName == indexName {
					found = true
				}
			}
			if !found {
				charts = append(charts, &ChartInfo{Name: chart.Name, Version: chart.Version, IndexName: indexName})
			}
		} else {
			logFields := map[string]interface{}{
				"application": appName.String(),
			}
			monitor.WithFields(logFields).Info("Not helm templated")
		}
	}

	monitor.Info("Adding all indexes")
	// add all indexes in a map so that no dublicates exist
	for _, v := range indexes {
		if err := addIndex(monitor, basePath, v); err != nil {
			return err
		}
	}

	monitor.Info("Repo update")
	if err := helmcommand.RepoUpdate(basePath); err != nil {
		return err
	}

	if newVersions {
		monitor.Info("Checking newer chart versions")
		if err := CompareVersions(monitor, basePath, charts); err != nil {
			return err
		}
	}

	monitor.Info("Fetching all charts")
	for _, chart := range charts {
		if err := fetch(monitor, basePath, chart); err != nil {
			return err
		}
	}
	return nil
}

func fetch(monitor mntr.Monitor, basePath string, chart *ChartInfo) error {
	logFields := map[string]interface{}{
		"application": chart.Name,
		"version":     chart.Version,
		"indexname":   chart.IndexName,
		"folder":      basePath,
	}

	monitor.WithFields(logFields).Info("Fetching chart")
	return helmcommand.FetchChart(&helmcommand.FetchConfig{
		TempFolderPath: basePath,
		ChartName:      chart.Name,
		ChartVersion:   chart.Version,
		IndexName:      chart.IndexName,
	})
}

func addIndex(monitor mntr.Monitor, basePath string, index *chart.Index) error {
	logFields := map[string]interface{}{
		"index": index.Name,
		"url":   index.URL,
	}
	monitor.WithFields(logFields).Info("Adding index")
	return helmcommand.AddIndex(&helmcommand.IndexConfig{
		TempFolderPath: basePath,
		IndexName:      index.Name,
		IndexURL:       index.URL,
	})
}
