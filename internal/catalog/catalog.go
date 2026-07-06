package catalog

const Version = "v1"

var ReadOnlyOperations = []string{
	"system_overview",
	"disk_usage",
	"memory_usage",
	"top_processes",
	"service_status",
	"service_logs",
	"health_check",
	"target_probe",
}

var RemediationOperations = []string{
	"restart_service",
	"recover_demo_app",
	"reload_service",
}

func IsReadOnly(operation string) bool {
	for _, candidate := range ReadOnlyOperations {
		if operation == candidate {
			return true
		}
	}
	return false
}

func IsRemediation(operation string) bool {
	for _, candidate := range RemediationOperations {
		if operation == candidate {
			return true
		}
	}
	return false
}
