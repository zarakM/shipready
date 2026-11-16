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

Exit code is 0 when the scan completes, regardless of findings. Add `--fail-on critical|high` for CI use: the report and summary are still written, and the exit code is 2 when any check of that severity or above failed. Exit code 1 means the scan itself could not run.

`--aws` is not registered until milestone 4.

Output rules (decided in milestone 3):

- With one format, the report is written to `--output` exactly as given. With `--format html,json`, each format gets `--output`'s name with its own extension, so the default writes `shipready-report.html` and `shipready-report.json` side by side.
- `--output something.json` with no `--format` means JSON. An explicit `--format` always wins.
- **Never half a report.** A check that fails, or a resource type that cannot be read, never stops the scan: every check is still run and reported. All formats are rendered in memory before any file is written, each file is moved into place in one step, and if one format cannot be written the others are removed. `--fail-on` changes only the exit code; the complete report and summary are written first.

`--skip` accepts check IDs and category names, case-insensitive. A check skipped this way still appears in the report as skipped, with the reason.

With `--namespace`, checks that need to see the whole cluster (COST-002, COST-004, REC-001) are skipped with a reason. Cluster-scoped checks such as REL-008 and SEC-002 still run.

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
| REL-009 | TLS certificates expiring within 30 days (read from cert-manager; Secrets are never opened) | high |
| REL-010 | Images using the `latest` tag or no tag | medium |

Scope rules for the reliability checks (decided in milestone 1):

- **Workloads** means Deployments, StatefulSets, DaemonSets, CronJobs and bare Pods (Pods with no owner that have not finished). Findings are reported once per controller, not once per replica.
- REL-001 and REL-002 cover all workloads. REL-003 covers Deployments and StatefulSets only. REL-004 covers all workloads except CronJobs, because liveness probes rarely make sense for batch jobs.
- Only regular containers are checked. Init containers and ephemeral containers are ignored.
- REL-001 fails a container when either its CPU or its memory request is missing. A resource with a limit but no request counts as having a request, because Kubernetes copies the limit into the request when the pod is created.
- REL-005: "multi-replica" means `spec.replicas >= 2` on a Deployment or StatefulSet. It passes when any PodDisruptionBudget in the same namespace has a selector that matches the pod template labels.
- A check is skipped, never partially run, when any resource type it needs could not be listed.

Decided in milestone 2:

- **REL-006:** every Deployment and StatefulSet with exactly one replica is flagged, except in namespaces whose name marks them as non-production (`dev`, `staging`, `stage`, `test`, `qa`, `sandbox`, `preview` and close variants, as a dash-separated word). A workload whose autoscaler has a minimum of 2 or more passes; one whose autoscaler minimum is 1 is still flagged, with the advice to raise the minimum to 2.
- **REL-007:** a Deployment with no HorizontalPodAutoscaler targeting it. Deployments scaled to zero are exempt.
- **REL-008:** fails when every node with a zone label is in the same zone. Skipped when there are no nodes or no node carries a zone label.
- **REL-009:** expiry is read from the status of cert-manager `Certificate` objects. The scanner never lists or reads Secrets, so without cert-manager this check is skipped. A certificate is flagged when it expires within 30 days, unless cert-manager has a renewal scheduled that is not yet due (short-lived certificates working as designed).
- **REL-010:** an image with no tag or the `latest` tag, unless it is pinned by digest. Init containers are included.

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

Scope rules for the security checks (decided in milestone 2):

- Security checks look at init containers as well as regular containers, because a privileged init container is a real risk.
- **SEC-001:** fails a container that is privileged, that sets `runAsUser: 0`, or that has nothing preventing root (no `runAsNonRoot: true` and no non-zero `runAsUser`, at pod or container level). Container settings override pod settings.
- **SEC-002:** subjects treated as system and exempt are the `system:masters` group, `system:` users other than `system:anonymous`, `eks:` users, and ServiceAccounts in `kube-system`. Every other subject is flagged, including catch-all groups such as `system:authenticated`.
- **SEC-003:** Roles and ClusterRoles with `*` in verbs or resources. Built-in roles (labelled `kubernetes.io/bootstrapping=rbac-defaults`, or named `system:` or `eks:`) are exempt.
- **SEC-004:** an environment variable whose name contains `PASSWORD`, `PASSWD`, `SECRET`, `TOKEN`, `API_KEY`, `PRIVATE_KEY`, `ACCESS_KEY` or `CREDENTIAL` and which has a literal value. Names ending in `_FILE`, `_PATH`, `_DIR`, `_NAME`, `_URL`, `_URI` or `_ENDPOINT` are exempt. Only the variable name is ever reported.
- **SEC-005:** only namespaces that run at least one workload are judged.
- **SEC-006:** a LoadBalancer Service is public unless it is marked internal by annotation, is built by the AWS Load Balancer Controller without `internet-facing`, or restricts source ranges to something narrower than `0.0.0.0/0`. A NodePort Service is flagged only when at least one node has an external IP.
- **SEC-007:** `hostNetwork`, `hostPID` or any `hostPath` volume.
- **SEC-008:** the workload uses the `default` ServiceAccount and token mounting is not turned off on the pod or on that ServiceAccount.

### Cost (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| COST-001 | Requests far above actual usage (needs metrics-server; skip if absent) | medium |
| COST-002 | Nodes with low allocation (under 30% requested) | medium |
| COST-003 | PersistentVolumes in Released or unbound state | low |
| COST-004 | No cluster autoscaler or Karpenter detected | medium |

Scope rules for the cost checks (decided in milestone 2):

- **COST-001:** metrics-server gives a single point-in-time sample, so the rule is conservative. A Deployment, StatefulSet or DaemonSet fails when its running pods together use under 25% of their requested CPU or memory. Requests under 100m CPU or 128Mi memory are ignored, and so are pods younger than 10 minutes.
- **COST-002:** a node fails when both its CPU and its memory requests are under 30% of allocatable. Finished pods do not count.
- **COST-003:** PersistentVolumes in the `Released`, `Available` or `Failed` phase.
- **COST-004:** passes when the `karpenter.sh` API is installed (this covers EKS Auto Mode) or a Deployment in any namespace is named for, or runs an image of, `cluster-autoscaler` or `karpenter`.

### Recovery (Kubernetes)

| ID | Check | Severity |
| --- | --- | --- |
| REC-001 | No backup tool detected (Velero or equivalent) | critical |
| REC-002 | Velero installed but no successful backup in the last 7 days | high |
| REC-003 | StatefulSets with no VolumeSnapshot or backup schedule covering them | high |

Scope rules for the recovery checks (decided in milestone 2):

- **REC-001:** a backup tool is detected by its API group: Velero, Kasten K10, Stash, KubeStash or TrilioVault. With no tool installed, the check fails only when the scanned namespaces hold PersistentVolumeClaims; a cluster with no persistent data passes. Backups taken outside the cluster, such as AWS Backup, are not visible until the AWS checks in milestone 4.
- **REC-002:** fails when no Velero backup has reached `Completed` in the last 7 days. Skipped when Velero is not installed.
- **REC-003:** applies to StatefulSets with volume claim templates. One is covered when an unpaused Velero schedule includes its namespace (and matches its pod labels, if the schedule has a label selector), or when every one of its volumes has a ready VolumeSnapshot from the last 7 days. It runs even when no backup tool is installed, so such a cluster fails both REC-001 and REC-003.

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
- A category in which every check was skipped has no score. It is shown as not available and left out of the overall average. If no category has a score, there is no overall score.
- Top priorities are the failed checks ordered by severity, then by number of affected resources, then by ID.

## 8. Report

One self-contained HTML file, dark technical look to match the Opscify site.

1. **Header:** cluster name, scan time, ShipReady version, overall score and grade.
2. **Category cards:** four scores with pass/fail/skipped counts.
3. **Top priorities:** the five highest-severity findings with one-line fixes.
4. **Findings by category:** collapsible; each shows why it matters, affected resources (namespace/kind/name) and a remediation snippet.
5. **Skipped checks:** with reasons, so the user knows what was not covered.
6. **Call to action:** "Want this fixed in a week? Book a call." Link comes from a build-time variable `CTAURL` (placeholder until the Opscify booking link exists).

The JSON output carries the same data in a stable schema so a paid tier can consume it later.

Decided in milestone 3:

- **Look:** a neutral dark theme, since the Opscify brand assets are not in the repo yet. Every colour is a variable at the top of `internal/report/assets/report.css`, so matching the site later is a change in one place. Fonts are the reader's system fonts; nothing is downloaded.
- **Self-contained and offline:** CSS and JS are embedded in the binary and inlined. The file carries a Content-Security-Policy that blocks every external request, so the browser enforces that nothing leaves the machine. The only outbound link is the call to action.
- **Works without scripts:** findings collapse with native `<details>`. The inline script only adds a severity filter, expand and collapse all, copy buttons on snippets, opening a finding when its link is followed, and expanding everything for print.
- **Findings:** ordered by severity within each category. Every affected resource is listed, in a scrolling table. Passed checks are listed per category in a collapsed block.
- **Print:** a light print stylesheet, so the report can be saved as PDF and forwarded.
- **Call to action:** the text is always shown and does not mention the price. The "Book a call" button appears only when `CTAURL` is set at build time to an http(s) link (`-ldflags "-X main.CTAURL=..."`), so a report never carries a dead link.
- **Sample:** `docs/sample-report.html` is rendered from `testdata/sample/cluster.yaml`, a fixture built to give a believable mixed result. A test fails when the committed file is out of date; `go test ./internal/report -run TestSampleReportIsCurrent -update` regenerates it. Its button points at a placeholder link until the booking URL exists.

## 9. Architecture

```
collect  ->  checks  ->  score  ->  report
```

- **collect:** fetches everything once into an in-memory `Snapshot` (workloads, pods, nodes, RBAC, services, ingresses, PVs, HPAs, PDBs, network policies, optional metrics). Checks never call the API directly, which keeps them fast and easy to test.
- **checks:** `type Check interface { ID() string; Meta() Meta; Run(*Snapshot) Result }`. Registered in a central registry.
- **score:** pure functions over results.
- **report:** renderers for terminal summary, HTML and JSON.

System namespaces (`kube-system`, `kube-public`, `kube-node-lease`) are excluded from workload checks by default. The `Snapshot` still holds their objects on a whole-cluster scan, so that checks can detect tools installed there (an autoscaler, for example) and count every pod on a node. Naming a system namespace with `--namespace` puts it in scope.

The collector never lists Secrets. Optional add-on APIs (metrics-server, cert-manager, Velero, VolumeSnapshots) are read through the dynamic client, and only when API discovery shows they are installed.

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
