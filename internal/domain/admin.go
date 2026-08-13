package domain

import "time"

type AdminSession struct {
	TokenHash []byte
	CSRFHash  []byte
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type AdminAudit struct {
	Username   string
	Action     string
	ReportID   *string
	RemoteHash []byte
	CreatedAt  time.Time
}

type AdminReportSummary struct {
	ID             string       `json:"id"`
	SupportCode    string       `json:"supportCode"`
	ProductID      string       `json:"productId"`
	RequestType    RequestType  `json:"requestType"`
	Status         ReportStatus `json:"status"`
	Source         string       `json:"source"`
	Title          string       `json:"title"`
	HasDiagnostics bool         `json:"hasDiagnostics"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	RetentionUntil time.Time    `json:"retentionUntil"`
}

type AdminReportDetail struct {
	AdminReportSummary
	Description string          `json:"description"`
	Contact     string          `json:"contact,omitempty"`
	Release     ReleaseMetadata `json:"release"`
}
