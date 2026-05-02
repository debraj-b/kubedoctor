<p align="center">
  <img src="assets/kubedoctor.png" alt="KubeDoctor" width="200"/>
</p>

<h1 align="center">KubeDoctor</h1>

<p align="center">
  A fast, lightweight CLI health checker for Kubernetes clusters.
  <br/>
  Scans your cluster and reports issues across pods, deployments, and services — in seconds.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/built%20with-Go-00ADD8?style=flat-square&logo=go" />
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" />
  <img src="https://img.shields.io/badge/install-Homebrew-FBB040?style=flat-square&logo=homebrew" />
</p>

---

## Install

```bash
brew install kubedoctor
```

---

## Usage

```bash
# Scan your current cluster
kubedoctor run

# Scan a specific namespace
kubedoctor run --namespace production

# Scan a specific context
kubedoctor run --context my-cluster

# Scan with more workers (faster on large clusters)
kubedoctor run --workers 20
```

---

## What It Checks

**Pods**
- `CrashLoopBackOff` — with structured crash log summary (exit code, error pattern, last log line)
- `OOMKilled` — container exceeded memory limits
- High restart counts
- Pods stuck in `Pending`
- Missing resource requests / limits
- Missing liveness and readiness probes

**Deployments**
- Unavailable replicas
- Single replica deployments (no redundancy)
- Stalled rollouts
- Missing rolling update strategy

**Services**
- Selector matches no pods (traffic silently dropped)
- Zero ready endpoints
- Port mismatches between service and pods

---

## Output

Findings are grouped by namespace, sorted by severity (`CRITICAL` → `WARNING` → `INFO`).

For critical pod failures, KubeDoctor shows a structured crash summary:

```
 Pod       : crashloop-app
 Exit Code : 1 (general error)
 Pattern   : connection refused
 Last Log  : ERROR: cannot connect to database at postgres:5432
```

---

## Benchmark

Tested against a real Kind cluster with 500 pods across 1 namespace:

| Pods | Total Time | Throughput     |
|------|------------|----------------|
| 500  | 60ms       | 8,313 pods/sec |

---

## Requirements

- `kubectl` configured with a valid kubeconfig (`~/.kube/config`)
- Kubernetes cluster (local or remote)

---

## License

MIT
