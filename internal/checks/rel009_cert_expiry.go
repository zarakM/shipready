package checks

import (
	"fmt"
	"time"

	"github.com/zarakM/shipready/internal/collect"
)

const certWarningWindow = 30 * 24 * time.Hour

var rel009 = check{
	id: "REL-009",
	meta: Meta{
		Category: Reliability,
		Severity: High,
		Title:    "TLS certificates expiring within 30 days",
		Description: "A certificate that expires takes the site down for every visitor with a browser security warning. " +
			"cert-manager normally renews well ahead of time, so a certificate this close to expiry usually means renewal is failing.",
		Remediation: "Check why renewal is stuck with `kubectl describe certificate <name>`. Common causes are a failing DNS or HTTP challenge and an issuer that is not ready.",
	},
	run: func(s *collect.Snapshot) Result {
		// Expiry is read from cert-manager's Certificate status. Reading it
		// from the TLS Secret would mean fetching the private key too, which
		// ShipReady never does.
		if !s.HasAPI(collect.GroupCertManager) {
			return skipped("cert-manager is not installed; ShipReady never opens Secrets, so it cannot read certificate expiry any other way")
		}
		if reason := s.Unavailable(collect.ResCertificates); reason != "" {
			return skipped(reason)
		}
		now := s.CollectedAt
		var found []Resource
		for _, c := range s.Certificates {
			if !s.InScope(c.Namespace) || c.NotAfter.IsZero() {
				continue
			}
			left := c.NotAfter.Sub(now)
			if left >= certWarningWindow {
				continue
			}
			// A short-lived certificate whose renewal is scheduled and not
			// yet due is working as designed.
			if left > 0 && !c.RenewalTime.IsZero() && now.Before(c.RenewalTime) {
				continue
			}
			found = append(found, Resource{
				Namespace: c.Namespace, Kind: "Certificate", Name: c.Name, Detail: expiryDetail(left),
			})
		}
		return fromResources(found)
	},
}

func expiryDetail(left time.Duration) string {
	days := int(left.Abs().Hours() / 24)
	unit := "days"
	if days == 1 {
		unit = "day"
	}
	switch {
	case left <= 0 && days == 0:
		return "expired today"
	case left <= 0:
		return fmt.Sprintf("expired %d %s ago", days, unit)
	case days == 0:
		return "expires in less than a day"
	default:
		return fmt.Sprintf("expires in %d %s", days, unit)
	}
}
