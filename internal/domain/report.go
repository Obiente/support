package domain

import "time"

const (
	ContractVersion           = 1
	MaxMetadataBytes          = 32 * 1024
	MaxDiagnosticArchiveBytes = 4 * 1024 * 1024
)

func (value ReportStatus) Valid() bool {
	return value == StatusNew || value == StatusNeedsInformation || value == StatusAccepted ||
		value == StatusDuplicate || value == StatusResolved || value == StatusRejected
}

type RequestType string

const (
	RequestBug     RequestType = "bug"
	RequestFeature RequestType = "feature"
	RequestSupport RequestType = "support"
)

func (value RequestType) Valid() bool {
	return value == RequestBug || value == RequestFeature || value == RequestSupport
}

type ReportStatus string

const (
	StatusNew              ReportStatus = "new"
	StatusNeedsInformation ReportStatus = "needs_information"
	StatusAccepted         ReportStatus = "accepted"
	StatusDuplicate        ReportStatus = "duplicate"
	StatusResolved         ReportStatus = "resolved"
	StatusRejected         ReportStatus = "rejected"
)

type ReleaseMetadata struct {
	Version      string `json:"version"`
	Channel      string `json:"channel,omitempty"`
	Platform     string `json:"platform"`
	OSVersion    string `json:"osVersion,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

type ReportMetadata struct {
	ContractVersion int             `json:"contractVersion"`
	ProductID       string          `json:"productId"`
	RequestType     RequestType     `json:"requestType"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Contact         string          `json:"contact,omitempty"`
	Source          string          `json:"source"`
	Release         ReleaseMetadata `json:"release"`
	PrivacyAccepted bool            `json:"privacyAccepted"`
}

type PrivatePayload struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Contact     string          `json:"contact,omitempty"`
	Source      string          `json:"source"`
	Release     ReleaseMetadata `json:"release"`
}

type Report struct {
	ID                   string
	SupportCode          string
	ProductID            string
	RequestType          RequestType
	Status               ReportStatus
	PrivatePayload       []byte
	CapabilityCiphertext []byte
	DiagnosticObjectKey  *string
	IdempotencyHash      []byte
	RequestHash          []byte
	CapabilityHash       []byte
	CreatedAt            time.Time
	UpdatedAt            time.Time
	RetentionUntil       time.Time
	DeletedAt            *time.Time
}

type Receipt struct {
	ContractVersion int       `json:"contractVersion"`
	SupportCode     string    `json:"supportCode"`
	Status          string    `json:"status"`
	StatusURL       string    `json:"statusUrl"`
	DeletionURL     string    `json:"deletionUrl"`
	CreatedAt       time.Time `json:"createdAt"`
	RetentionUntil  time.Time `json:"retentionUntil"`
}

type PrivateStatus struct {
	ContractVersion int       `json:"contractVersion"`
	SupportCode     string    `json:"supportCode"`
	ProductID       string    `json:"productId"`
	RequestType     string    `json:"requestType"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	RetentionUntil  time.Time `json:"retentionUntil"`
}
