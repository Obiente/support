package intake

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/products"
	"github.com/obiente/support/internal/store"
)

var (
	ErrInvalid      = errors.New("invalid report")
	ErrNotFound     = errors.New("report not found")
	ErrKeyReused    = errors.New("idempotency key was already used for another report")
	idempotencyKey  = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)
	productID       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)
	archiveFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
)

type Submission struct {
	Metadata       domain.ReportMetadata
	Archive        []byte
	ArchiveType    string
	IdempotencyKey string
}

type Service struct {
	reports   store.Reports
	objects   store.Objects
	products  *products.Registry
	box       *cryptobox.Box
	publicURL string
	now       func() time.Time
}

func New(reports store.Reports, objects store.Objects, registry *products.Registry, box *cryptobox.Box, publicURL string) *Service {
	return &Service{
		reports: reports, objects: objects, products: registry, box: box,
		publicURL: strings.TrimSuffix(publicURL, "/"), now: func() time.Time { return time.Now().UTC() },
	}
}

func (service *Service) Submit(ctx context.Context, submission Submission) (domain.Receipt, error) {
	metadata, product, err := service.validate(submission)
	if err != nil {
		return domain.Receipt{}, err
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return domain.Receipt{}, err
	}
	requestHash := hashRequest(metadataBytes, submission.Archive)
	idempotencyHash := hash([]byte(submission.IdempotencyKey))
	if existing, err := service.reports.ByIdempotencyHash(ctx, idempotencyHash); err == nil {
		if !bytes.Equal(existing.RequestHash, requestHash) {
			return domain.Receipt{}, ErrKeyReused
		}
		return service.receipt(existing)
	} else if !errors.Is(err, store.ErrNotFound) {
		return domain.Receipt{}, err
	}

	privatePayload, err := json.Marshal(domain.PrivatePayload{
		Title: metadata.Title, Description: metadata.Description, Contact: metadata.Contact,
		Source: metadata.Source, Release: metadata.Release,
	})
	if err != nil {
		return domain.Receipt{}, err
	}

	for attempt := 0; attempt < 4; attempt++ {
		reportID, err := randomUUID()
		if err != nil {
			return domain.Receipt{}, err
		}
		capability, err := randomToken(32)
		if err != nil {
			return domain.Receipt{}, err
		}
		supportCode, err := randomSupportCode()
		if err != nil {
			return domain.Receipt{}, err
		}
		sealedPayload, err := service.box.Seal(privatePayload, []byte(reportID+":payload"))
		if err != nil {
			return domain.Receipt{}, err
		}
		sealedCapability, err := service.box.Seal([]byte(capability), []byte(reportID+":capability"))
		if err != nil {
			return domain.Receipt{}, err
		}
		now := service.now()
		report := domain.Report{
			ID: reportID, SupportCode: supportCode, ProductID: product.ID,
			RequestType: metadata.RequestType, Status: domain.StatusNew,
			PrivatePayload: sealedPayload, CapabilityCiphertext: sealedCapability,
			IdempotencyHash: idempotencyHash, RequestHash: requestHash,
			CapabilityHash: hash([]byte(capability)), CreatedAt: now, UpdatedAt: now,
			RetentionUntil: now.Add(time.Duration(product.RetentionDays) * 24 * time.Hour),
		}
		if len(submission.Archive) > 0 {
			objectKeyBytes := make([]byte, 32)
			if _, err := rand.Read(objectKeyBytes); err != nil {
				return domain.Receipt{}, err
			}
			objectKey := hex.EncodeToString(objectKeyBytes) + ".enc"
			if err := service.objects.Put(objectKey, reportID, submission.Archive); err != nil {
				return domain.Receipt{}, err
			}
			report.DiagnosticObjectKey = &objectKey
		}
		if err := service.reports.Create(ctx, report); err != nil {
			if report.DiagnosticObjectKey != nil {
				_ = service.objects.Delete(*report.DiagnosticObjectKey)
			}
			if errors.Is(err, store.ErrConflict) {
				if existing, lookupErr := service.reports.ByIdempotencyHash(ctx, idempotencyHash); lookupErr == nil {
					if !bytes.Equal(existing.RequestHash, requestHash) {
						return domain.Receipt{}, ErrKeyReused
					}
					return service.receipt(existing)
				}
				continue
			}
			return domain.Receipt{}, err
		}
		return service.receipt(report)
	}
	return domain.Receipt{}, errors.New("could not allocate a unique support receipt")
}

func (service *Service) Reconcile(ctx context.Context, key string) (domain.Receipt, error) {
	if !idempotencyKey.MatchString(key) {
		return domain.Receipt{}, ErrInvalid
	}
	report, err := service.reports.ByIdempotencyHash(ctx, hash([]byte(key)))
	if errors.Is(err, store.ErrNotFound) {
		return domain.Receipt{}, ErrNotFound
	}
	if err != nil {
		return domain.Receipt{}, err
	}
	return service.receipt(report)
}

func (service *Service) Status(ctx context.Context, capability string) (domain.PrivateStatus, error) {
	if !validCapability(capability) {
		return domain.PrivateStatus{}, ErrNotFound
	}
	report, err := service.reports.ByCapabilityHash(ctx, hash([]byte(capability)))
	if errors.Is(err, store.ErrNotFound) {
		return domain.PrivateStatus{}, ErrNotFound
	}
	if err != nil {
		return domain.PrivateStatus{}, err
	}
	return privateStatus(report), nil
}

func (service *Service) Delete(ctx context.Context, capability string) (domain.PrivateStatus, error) {
	if !validCapability(capability) {
		return domain.PrivateStatus{}, ErrNotFound
	}
	report, err := service.reports.DeleteByCapabilityHash(ctx, hash([]byte(capability)))
	if errors.Is(err, store.ErrNotFound) {
		return domain.PrivateStatus{}, ErrNotFound
	}
	if err != nil {
		return domain.PrivateStatus{}, err
	}
	if report.DiagnosticObjectKey != nil {
		if err := service.objects.Delete(*report.DiagnosticObjectKey); err != nil {
			return domain.PrivateStatus{}, fmt.Errorf("delete private diagnostic object: %w", err)
		}
	}
	status := privateStatus(report)
	status.Status = "deleted"
	return status, nil
}

func (service *Service) PurgeExpired(ctx context.Context, limit int) error {
	if limit < 1 || limit > 1000 {
		return errors.New("invalid retention purge limit")
	}
	now := service.now()
	reports, err := service.reports.Expired(ctx, now, limit)
	if err != nil {
		return err
	}
	var purgeErrors []error
	for _, report := range reports {
		if report.DiagnosticObjectKey != nil {
			if err := service.objects.Delete(*report.DiagnosticObjectKey); err != nil {
				purgeErrors = append(purgeErrors, fmt.Errorf("delete diagnostic object for %s: %w", report.ID, err))
				continue
			}
		}
		if err := service.reports.Purge(ctx, report.ID, now); err != nil && !errors.Is(err, store.ErrNotFound) {
			purgeErrors = append(purgeErrors, fmt.Errorf("purge report %s: %w", report.ID, err))
		}
	}
	return errors.Join(purgeErrors...)
}

func (service *Service) AdminList(ctx context.Context, status *domain.ReportStatus, limit, offset int) ([]domain.AdminReportSummary, int, error) {
	if limit < 1 || limit > 100 || offset < 0 || status != nil && !status.Valid() {
		return nil, 0, ErrInvalid
	}
	reports, total, err := service.reports.AdminList(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	result := make([]domain.AdminReportSummary, 0, len(reports))
	for _, report := range reports {
		payload, openErr := service.openPrivatePayload(report)
		if openErr != nil {
			return nil, 0, openErr
		}
		result = append(result, adminSummary(report, payload))
	}
	return result, total, nil
}

func (service *Service) AdminDetail(ctx context.Context, id string) (domain.AdminReportDetail, error) {
	report, err := service.reports.AdminByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return domain.AdminReportDetail{}, ErrNotFound
	}
	if err != nil {
		return domain.AdminReportDetail{}, err
	}
	payload, err := service.openPrivatePayload(report)
	if err != nil {
		return domain.AdminReportDetail{}, err
	}
	return domain.AdminReportDetail{
		AdminReportSummary: adminSummary(report, payload),
		Description:        payload.Description,
		Contact:            payload.Contact,
		Release:            payload.Release,
	}, nil
}

func (service *Service) AdminDiagnostics(ctx context.Context, id string) ([]byte, string, error) {
	report, err := service.reports.AdminByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) || err == nil && report.DiagnosticObjectKey == nil {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	content, err := service.objects.Get(*report.DiagnosticObjectKey, report.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", ErrNotFound
	}
	return content, report.SupportCode + "-diagnostics.zip", err
}

func (service *Service) AdminUpdateStatus(ctx context.Context, id string, status domain.ReportStatus) (domain.AdminReportDetail, error) {
	if !status.Valid() {
		return domain.AdminReportDetail{}, ErrInvalid
	}
	report, err := service.reports.AdminUpdateStatus(ctx, id, status, service.now())
	if errors.Is(err, store.ErrNotFound) {
		return domain.AdminReportDetail{}, ErrNotFound
	}
	if err != nil {
		return domain.AdminReportDetail{}, err
	}
	payload, err := service.openPrivatePayload(report)
	if err != nil {
		return domain.AdminReportDetail{}, err
	}
	return domain.AdminReportDetail{
		AdminReportSummary: adminSummary(report, payload),
		Description:        payload.Description,
		Contact:            payload.Contact,
		Release:            payload.Release,
	}, nil
}

func (service *Service) openPrivatePayload(report domain.Report) (domain.PrivatePayload, error) {
	plaintext, err := service.box.Open(report.PrivatePayload, []byte(report.ID+":payload"))
	if err != nil {
		return domain.PrivatePayload{}, err
	}
	var payload domain.PrivatePayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return domain.PrivatePayload{}, err
	}
	return payload, nil
}

func adminSummary(report domain.Report, payload domain.PrivatePayload) domain.AdminReportSummary {
	return domain.AdminReportSummary{
		ID: report.ID, SupportCode: report.SupportCode, ProductID: report.ProductID,
		RequestType: report.RequestType, Status: report.Status, Source: payload.Source,
		Title: payload.Title, HasDiagnostics: report.DiagnosticObjectKey != nil,
		CreatedAt: report.CreatedAt, UpdatedAt: report.UpdatedAt, RetentionUntil: report.RetentionUntil,
	}
}

func (service *Service) receipt(report domain.Report) (domain.Receipt, error) {
	capability, err := service.box.Open(report.CapabilityCiphertext, []byte(report.ID+":capability"))
	if err != nil {
		return domain.Receipt{}, err
	}
	statusURL := service.publicURL + "/r/" + url.PathEscape(string(capability))
	return domain.Receipt{
		ContractVersion: domain.ContractVersion, SupportCode: report.SupportCode,
		Status: string(report.Status), StatusURL: statusURL, DeletionURL: statusURL,
		CreatedAt: report.CreatedAt, RetentionUntil: report.RetentionUntil,
	}, nil
}

func (service *Service) validate(submission Submission) (domain.ReportMetadata, products.Product, error) {
	metadata := submission.Metadata
	metadata.ProductID = strings.TrimSpace(metadata.ProductID)
	metadata.Title = strings.TrimSpace(metadata.Title)
	metadata.Description = strings.TrimSpace(metadata.Description)
	metadata.Contact = strings.TrimSpace(metadata.Contact)
	metadata.Source = strings.TrimSpace(metadata.Source)
	metadata.Release.Version = strings.TrimSpace(metadata.Release.Version)
	metadata.Release.Channel = strings.TrimSpace(metadata.Release.Channel)
	metadata.Release.Platform = strings.TrimSpace(metadata.Release.Platform)
	metadata.Release.OSVersion = strings.TrimSpace(metadata.Release.OSVersion)
	metadata.Release.Architecture = strings.TrimSpace(metadata.Release.Architecture)
	if metadata.ContractVersion != domain.ContractVersion || !idempotencyKey.MatchString(submission.IdempotencyKey) ||
		!productID.MatchString(metadata.ProductID) || !metadata.RequestType.Valid() || !metadata.PrivacyAccepted ||
		(metadata.Source != "web" && metadata.Source != "app") || !validText(metadata.Title, 4, 160, false) ||
		!validText(metadata.Description, 10, 8000, true) || !validText(metadata.Contact, 0, 320, false) ||
		!validText(metadata.Release.Version, 0, 80, false) || !validText(metadata.Release.Channel, 0, 40, false) ||
		!validText(metadata.Release.Platform, 0, 60, false) || !validText(metadata.Release.OSVersion, 0, 120, false) ||
		!validText(metadata.Release.Architecture, 0, 40, false) {
		return domain.ReportMetadata{}, products.Product{}, ErrInvalid
	}
	product, ok := service.products.Get(metadata.ProductID)
	if !ok {
		return domain.ReportMetadata{}, products.Product{}, ErrInvalid
	}
	if len(submission.Archive) > 0 {
		if int64(len(submission.Archive)) > product.DiagnosticMaxBytes || !contains(product.DiagnosticContentTypes, submission.ArchiveType) {
			return domain.ReportMetadata{}, products.Product{}, ErrInvalid
		}
		if err := validateArchive(submission.Archive, product); err != nil {
			return domain.ReportMetadata{}, products.Product{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	return metadata, product, nil
}

func validateArchive(content []byte, product products.Product) error {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return errors.New("diagnostic attachment is not a valid ZIP archive")
	}
	if len(archive.File) == 0 || len(archive.File) > 32 {
		return errors.New("diagnostic archive has an invalid entry count")
	}
	allowed := make(map[string]struct{}, len(product.DiagnosticEntries))
	for _, name := range product.DiagnosticEntries {
		allowed[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(archive.File))
	var expanded int64
	for _, entry := range archive.File {
		clean := path.Clean(entry.Name)
		if clean != entry.Name || !archiveFileName.MatchString(entry.Name) || entry.FileInfo().Mode()&0o170000 != 0 {
			return errors.New("diagnostic archive contains an unsafe entry")
		}
		if _, ok := allowed[entry.Name]; !ok {
			return errors.New("diagnostic archive contains an unregistered entry")
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return errors.New("diagnostic archive contains a duplicate entry")
		}
		seen[entry.Name] = struct{}{}
		reader, err := entry.Open()
		if err != nil {
			return errors.New("diagnostic archive entry could not be opened")
		}
		remaining := product.DiagnosticMaxExpanded - expanded
		written, copyErr := io.CopyN(io.Discard, reader, remaining+1)
		closeErr := reader.Close()
		expanded += written
		if copyErr != nil && !errors.Is(copyErr, io.EOF) || closeErr != nil || expanded > product.DiagnosticMaxExpanded {
			return errors.New("diagnostic archive expands beyond its limit")
		}
	}
	for _, required := range product.DiagnosticEntries {
		if _, ok := seen[required]; !ok {
			return errors.New("diagnostic archive is missing a required entry")
		}
	}
	return nil
}

func validText(value string, minimum, maximum int, multiline bool) bool {
	if !utf8.ValidString(value) || len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character == 0 || character == '\r' || !multiline && character == '\n' || character < 0x20 && character != '\n' && character != '\t' {
			return false
		}
	}
	return true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func privateStatus(report domain.Report) domain.PrivateStatus {
	return domain.PrivateStatus{
		ContractVersion: domain.ContractVersion, SupportCode: report.SupportCode,
		ProductID: report.ProductID, RequestType: string(report.RequestType),
		Status: string(report.Status), CreatedAt: report.CreatedAt, UpdatedAt: report.UpdatedAt,
		RetentionUntil: report.RetentionUntil,
	}
}

func validCapability(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func hash(value []byte) []byte {
	digest := sha256.Sum256(value)
	return digest[:]
}

func hashRequest(metadata, archive []byte) []byte {
	digest := sha256.New()
	digest.Write(metadata)
	digest.Write([]byte{0})
	digest.Write(archive)
	return digest.Sum(nil)
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomSupportCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	value := make([]byte, 10)
	random := make([]byte, len(value))
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for index := range value {
		value[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return "OBI-" + string(value[:5]) + "-" + string(value[5:]), nil
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
