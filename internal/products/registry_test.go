package products

import "testing"

func TestRegistryOnboardsSyntheticProductWithoutCodeChanges(t *testing.T) {
	registry, err := New([]Product{
		{ID: "synthetic-product", Name: "Synthetic product", RetentionDays: 7, DiagnosticEntries: []string{"diagnostic.txt"}},
		{ID: "another-product", Name: "Another product", RetentionDays: 14},
	})
	if err != nil {
		t.Fatal(err)
	}
	product, ok := registry.Get("synthetic-product")
	if !ok || product.DiagnosticEntries[0] != "diagnostic.txt" {
		t.Fatalf("synthetic product was not registered: %#v", product)
	}
	if len(registry.List()) != 2 {
		t.Fatalf("product count = %d, want 2", len(registry.List()))
	}
}
