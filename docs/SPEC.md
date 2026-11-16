# ShipReady: product spec (v1 MVP)

## 1. Summary

ShipReady is a command-line scanner that tells a startup what is wrong with its Kubernetes setup before something breaks. The user runs it on their own machine with read-only access. It produces one HTML report with a score and a prioritized fix list across four areas: reliability, security, cost and recovery.

## 2. Why it exists

- **User problem:** teams that outgrew their first setup do not know what is wrong with their cluster until an outage, a breach or a surprise bill.
- **Trust problem:** nobody wants to hand cluster credentials to a stranger. ShipReady runs locally and sends nothing anywhere.
- **Gap in existing tools:** k8sgpt, Kubescape and Popeye each cover part of this. None gives one scan across cost, security and reliability that ends in a fix plan from a real engineer.
- **Business role:** the free scan is the lead magnet for Opscify. Each report ends with a "Fix it for me" call to action for the Opscify flagship package (production Kubernetes, $5,000, delivered in one week).

## 3. Target user

CTO or lead engineer at a Series A startup running Kubernetes on AWS (EKS first; AKS later). Technical enough to run a CLI, too busy to do a manual audit.

## 4. User flow

1. Download one binary.
2. Run `shipready scan` using the current kubeconfig context.
3. See a short summary in the terminal: overall score, score per category, top five findings.
4. Open `shipready-report.html` for the full report.

## 5. CLI

```
shipready scan [flags]
  --context string       kubeconfig context (default: current)
  --namespace strings    limit to namespaces (default: all non-system)
  --aws                  include AWS checks (uses the default AWS credential chain)
  --output string        report path (default: ./shipready-report.html)
  --format strings       html, json (default: html)
  --skip strings         check IDs or categories to skip
shipready checks         list all checks with ID, category and severity
shipready version
```

Exit code is 0 when the scan completes, regardless of findings. Add `--fail-on critical|high` for CI use.

**Until milestone 3 ships the HTML renderer:** `--format` defaults to `json`, `--output` defaults to `./shipready-report.json`, and `--format html` returns a clear "not available yet" error. Both defaults flip to the values above in milestone 3. `--fail-on` arrives with scoring in milestone 2 and `--aws` is not registered until milestone 4.

`--skip` accepts check IDs and category names, case-insensitive. A check skipped this way still appears in the report as skipped, with the reason.

## 6. Checks

Each check has an ID, category, severity (critical, high, medium, low), a title, a description of why it matters, and a remediation with an example snippet where useful.

### Reliability (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| REL-001 | Containers without CPU/memory requests | high |
| REL-002 | Containers without memory limits | high |
| REL-003 | Deployments/StatefulSets without readiness probes | high |
| REL-004 | Workloads without liveness probes | medium |
| REL-005 | Multi-replica workloads without a PodDisruptionBudget | medium |
| REL-006 | Production workloads running a single replica | high |
| REL-007 | Deployments with no HorizontalPodAutoscaler | low |
| REL-008 | All nodes in a single availability zone | critical |
| REL-009 | TLS certificates in Secrets/Ingress expiring within 30 days (metadata and cert expiry only) | high |
| REL-010 | Images using the `latest` tag or no tag | medium |

Scope rules for the reliability checks (decided in milestone 1):

- **Workloads** means Deployments, StatefulSets, DaemonSets, CronJobs and bare Pods (Pods with no owner that have not finished). Findings are reported once per controller, not once per replica.
- REL-001 and REL-002 cover all workloads. REL-003 covers Deployments and StatefulSets only. REL-004 covers all workloads except CronJobs, because liveness probes rarely make sense for batch jobs.
- Only regular containers are checked. Init containers and ephemeral containers are ignored.
- REL-001 fails a container when either its CPU or its memory request is missing. A resource with a limit but no request counts as having a request, because Kubernetes copies the limit into the request when the pod is created.
- REL-005: "multi-replica" means `spec.replicas >= 2` on a Deployment or StatefulSet. It passes when any PodDisruptionBudget in the same namespace has a selector that matches the pod template labels.
- A check is skipped, never partially run, when any resource type it needs could not be listed.

### Security (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| SEC-001 | Containers running as root or privileged | high |
| SEC-002 | ClusterRoleBindings to `cluster-admin` beyond system subjects | critical |
| SEC-003 | Roles with wildcard verbs or resources | high |
| SEC-004 | Secrets passed as plain environment variable values (name pattern match, value never printed) | high |
| SEC-005 | Namespaces with no NetworkPolicy | medium |
| SEC-006 | Services of type LoadBalancer or NodePort exposed publicly | medium |
| SEC-007 | Pods using hostNetwork, hostPID or hostPath | high |
| SEC-008 | Workloads using the default ServiceAccount with token automount | low |

### Cost (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| COST-001 | Requests far above actual usage (needs metrics-server; skip if absent) | medium |
| COST-002 | Nodes with low allocation (under 30% requested) | medium |
| COST-003 | PersistentVolumes in Released or unbound state | low |
| COST-004 | No cluster autoscaler or Karpenter detected | medium |

### Recovery (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| REC-001 | No backup tool detected (Velero or equivalent) | critical |
| REC-002 | Velero installed but no successful backup in the last 7 days | high |
| REC-003 | StatefulSets with no VolumeSnapshot or backup schedule covering them | high |

### AWS (behind `--aws`, milestone 3)

| ID | Check | Severity |
| --- | --- | --- |
| AWS-001 | EKS API endpoint public with no CIDR restriction | high |
| AWS-002 | EKS control plane logging disabled | medium |
| AWS-003 | Unattached EBS volumes | low |
| AWS-004 | Load balancers with no healthy targets | low |
| AWS-005 | RDS instances without automated backups or with retention under 7 days | critical |
| AWS-006 | Node groups in a single AZ | critical |
| AWS-007 | EKS version out of standard support | high |

## 7. Scoring

- Severity weights: critical 10, high 5, medium 2, low 1.
- Each check is pass, fail or skipped. Skipped checks are left out of the score and listed separately.
- Category score = 100 x (1 - failed weight / total weight of checks that ran), rounded.
- Overall score = average of the four category scores.
- Grade bands: 90+ A, 75 to 89 B, 60 to 74 C, 40 to 59 D, under 40 F.
- A check that fails on many resources still counts once for scoring; the report lists every affected resource.

## 8. Report

One self-contained HTML file, dark technical look to match the Opscify site.

1. **Header:** cluster name, scan time, ShipReady version, overall score and grade.
2. **Category cards:** four scores with pass/fail/skipped counts.
3. **Top priorities:** the five highest-severity findings with one-line fixes.
4. **Findings by category:** collapsible; each shows why it matters, affected resources (namespace/kind/name) and a remediation snippet.
5. **Skipped checks:** with reasons, so the user knows what was not covered.
6. **Call to action:** "Want this fixed in a week? Book a call." Link comes from a build-time variable `CTAURL` (placeholder until the Opscify booking link exists).

The JSON output carries the same data in a stable schema so a paid tier can consume it later.

## 9. Architecture

```
collect  ->  checks  ->  score  ->  report
```

- **collect:** fetches everything once into an in-memory `Snapshot` (workloads, pods, nodes, RBAC, services, ingresses, PVs, HPAs, PDBs, network policies, optional metrics). Checks never call the API directly, which keeps them fast and easy to test.
- **checks:** `type Check interface { ID() string; Meta() Meta; Run(*Snapshot) Result }`. Registered in a central registry.
- **score:** pure functions over results.
- **report:** renderers for terminal summary, HTML and JSON.

System namespaces (`kube-system`, `kube-public`, `kube-node-lease`) are excluded from workload checks by default.

## 10. Milestones

1. **Skeleton:** repo layout, cobra CLI, kubeconfig loading, `Snapshot` collector, check interface and registry, REL-001 to REL-005 with tests, JSON output.
2. **Kubernetes checks complete:** remaining REL, SEC, COST and REC checks with tests, scoring, terminal summary.
3. **HTML report:** template, embedded assets, sample report generated from fixtures and committed under `docs/sample-report.html`.
4. **AWS checks:** `--aws` flag, AWS collector, AWS-001 to AWS-007 with tests against mocked clients.
5. **Release:** goreleaser config, README with install and usage, minimal read-only ClusterRole manifest and IAM policy the user can apply for the scanner.

## 11. Out of scope for v1

- Hosted backend, accounts, scheduled scans, history, Slack alerts (these are the paid tier, later)
- Azure and GCP checks
- Automatic fixes or any write operation
- Image vulnerability scanning (point users to Trivy instead)
- Terraform drift detection

## 12. Open questions

- Final product name and domain (ShipReady is a working name).
- Opscify booking URL for the call to action.
- Whether to reuse any code from the earlier aik8scopilot work.
