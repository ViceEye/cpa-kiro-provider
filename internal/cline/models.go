package cline

// modelsForAuth registers the curated free-tier catalog under the cline/
// prefix. Live discovery exists upstream (GET /models, ~396 entries) but the
// free tier only serves a subset; extend freeModels as needed.
func ModelsForAuth(raw []byte) ([]byte, error) {
	models := make([]modelInfo, 0, len(freeModels))
	for _, model := range freeModels {
		entry := model
		entry.ID = modelPrefix + model.ID
		models = append(models, entry)
	}
	return okEnvelope(modelResponse{Provider: providerID, Models: models})
}
