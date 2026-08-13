package products

import (
	"errors"
	"sort"
)

type Product struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	Repository             string   `json:"repository,omitempty"`
	DiagnosticContentTypes []string `json:"diagnosticContentTypes"`
	DiagnosticEntries      []string `json:"diagnosticEntries"`
	DiagnosticMaxBytes     int64    `json:"diagnosticMaxBytes"`
	DiagnosticMaxExpanded  int64    `json:"diagnosticMaxExpandedBytes"`
	RetentionDays          int      `json:"retentionDays"`
}

type Registry struct {
	products map[string]Product
}

func New(values []Product) (*Registry, error) {
	registry := &Registry{products: make(map[string]Product, len(values))}
	for _, product := range values {
		if product.ID == "" || product.Name == "" || product.RetentionDays < 1 {
			return nil, errors.New("invalid product registration")
		}
		if _, exists := registry.products[product.ID]; exists {
			return nil, errors.New("duplicate product registration")
		}
		registry.products[product.ID] = product
	}
	if len(registry.products) == 0 {
		return nil, errors.New("at least one product is required")
	}
	return registry, nil
}

func Default() *Registry {
	registry, err := New([]Product{
		{
			ID:                     "nextcloud-native",
			Name:                   "Nextcloud Native",
			Description:            "Native Nextcloud client for Android and desktop.",
			Repository:             "Obiente/nc-native",
			DiagnosticContentTypes: []string{"application/zip"},
			DiagnosticEntries:      []string{"README.txt", "report.json", "events.jsonl", "manifest.json"},
			DiagnosticMaxBytes:     4 * 1024 * 1024,
			DiagnosticMaxExpanded:  8 * 1024 * 1024,
			RetentionDays:          30,
		},
		{
			ID:                     "obiente-general",
			Name:                   "Other Obiente project",
			Description:            "Questions and requests that are not tied to a listed product.",
			DiagnosticContentTypes: []string{},
			DiagnosticEntries:      []string{},
			DiagnosticMaxBytes:     0,
			DiagnosticMaxExpanded:  0,
			RetentionDays:          30,
		},
	})
	if err != nil {
		panic(err)
	}
	return registry
}

func (registry *Registry) Get(id string) (Product, bool) {
	product, ok := registry.products[id]
	return product, ok
}

func (registry *Registry) List() []Product {
	result := make([]Product, 0, len(registry.products))
	for _, product := range registry.products {
		result = append(result, product)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}
