package app

import (
	"strings"

	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/cloudflare"
)

func (a *App) EnsureDNSRecords(domain string, records []*cloudflare.DNSRecord) error {

	currentRecords, err := a.cloudflare.GetDNSRecords(domain)
	if err != nil {
		return err
	}

	deleteRecords := getRecordsToDelete(currentRecords, records)
	if deleteRecords != nil && len(deleteRecords) > 0 {
		if err := a.cloudflare.DeleteDNSRecords(domain, deleteRecords); err != nil {
			return err
		}
	}

	createRecords, updateRecords := getRecordsToCreateAndUpdate(currentRecords, records)
	if createRecords != nil && len(createRecords) > 0 {
		_, err := a.cloudflare.CreateDNSRecords(domain, createRecords)
		if err != nil {
			return err
		}
	}

	if updateRecords != nil && len(updateRecords) > 0 {
		_, err := a.cloudflare.UpdateDNSRecords(domain, updateRecords)
		if err != nil {
			return err
		}
	}
	return nil
}

func getRecordsToDelete(currentRecords []*cloudflare.DNSRecord, records []*cloudflare.DNSRecord) []string {
	deleteRecords := make([]string, 0)

	for _, currentRecord := range currentRecords {
		found := false
		if records != nil {
			if currentRecord.Type == "MX" {
				for _, record := range records {
					if currentRecord.Type == record.Type &&
						currentRecord.Name == record.Name &&
						(record.Content == currentRecord.Content || strings.ToLower(record.Content) == currentRecord.Content) {
						found = true
					}
				}
			} else {
				for _, record := range records {
					if currentRecord.Type == record.Type && currentRecord.Name == record.Name {
						found = true
					}
				}
			}
		}

		if found == false {
			deleteRecords = append(deleteRecords, currentRecord.ID)
		}
	}

	return deleteRecords
}

func getRecordsToCreateAndUpdate(currentRecords []*cloudflare.DNSRecord, records []*cloudflare.DNSRecord) ([]*cloudflare.DNSRecord, []*cloudflare.DNSRecord) {
	createRecords := make([]*cloudflare.DNSRecord, 0)
	updateRecords := make([]*cloudflare.DNSRecord, 0)

	if records != nil {
		for _, record := range records {
			if record.Type == "MX" {
				found := false
				for _, currentRecord := range currentRecords {
					if record.Type == currentRecord.Type &&
						record.Name == currentRecord.Name &&
						(record.Content == currentRecord.Content || strings.ToLower(record.Content) == currentRecord.Content) {
						found = true
					}
				}
				if !found {
					createRecords = append(createRecords, record)
				}
			}
		}
		for _, record := range records {
			if record.Type != "MX" {
				found := false
				for _, currentRecord := range currentRecords {
					if record.Type == currentRecord.Type &&
						record.Name == currentRecord.Name {

						record.ID = currentRecord.ID
						if record.Content != currentRecord.Content ||
							record.TTL != currentRecord.TTL ||
							record.Proxied != currentRecord.Proxied ||
							record.Priority != currentRecord.Priority {
							updateRecords = append(updateRecords, record)
						}
						found = true
						break
					}
				}
				if found == false {
					createRecords = append(createRecords, record)
				}
			}
		}
	}

	return createRecords, updateRecords
}
