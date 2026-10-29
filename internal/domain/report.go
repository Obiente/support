package domain

import "time"

const (
	ContractVersion           = 1
	MaxMetadataBytes          = 32 * 1024
	MaxDiagnosticArchiveBytes = 4 * 1024 * 1024
	MaxMessageBytes           = 8 * 1024
)

// ReceiptLinkRetention is how long contract version 1 clients are told to keep
// their local copy of a private report link. It is not a server deletion date:
// reports are kept until the reporter or a maintainer deletes them. Released
// clients require a timestamp in this window, so the value stays in every
// contract version 1 response for wire compatibility.
const ReceiptLinkRetention = 30 * 24 * time.Hour

// ReceiptLinkRetentionUntil returns the compatibility value reported to
// contract version 1 clients as retentionUntil.
func ReceiptLinkRetentionUntil(createdAt time.Time) time.Time {
	return createdAt.Add(ReceiptLinkRetention)
}

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
	// RetentionUntil mirrors the nullable retention_until column. New reports
	// store NULL and nothing deletes a report because of its age.
	RetentionUntil *time.Time
	DeletedAt      *time.Time
}

// DiagnosticReference identifies the private object attached to a live report.
type DiagnosticReference struct {
	ReportID  string
	ObjectKey string
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
	Messages        []Message `json:"messages"`
}

type MessageAuthor string

const (
	MessageAuthorMaintainer MessageAuthor = "maintainer"
	MessageAuthorReporter   MessageAuthor = "reporter"
)

type Message struct {
	ID             string        `json:"id"`
	Author         MessageAuthor `json:"author"`
	Body           string        `json:"body"`
	CreatedAt      time.Time     `json:"createdAt"`
	BodyCiphertext []byte        `json:"-"`
	ReportID       string        `json:"-"`
}
