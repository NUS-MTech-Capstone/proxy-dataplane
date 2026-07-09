package catalog

import "testing"

func TestOperationCatalogPredicates(t *testing.T) {
	for _, operation := range ReadOnlyOperations {
		if !IsReadOnly(operation) {
			t.Fatalf("IsReadOnly(%q) = false", operation)
		}
		if IsRemediation(operation) {
			t.Fatalf("read-only operation %q is remediation", operation)
		}
	}
	for _, operation := range RemediationOperations {
		if !IsRemediation(operation) {
			t.Fatalf("IsRemediation(%q) = false", operation)
		}
	}
	for _, operation := range []string{"", "health_check ", "unknown"} {
		if IsReadOnly(operation) || IsRemediation(operation) {
			t.Fatalf("unknown operation %q matched catalog", operation)
		}
	}
}
