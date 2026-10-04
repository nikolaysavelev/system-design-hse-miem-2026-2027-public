# Тесты политик (conftest verify): каждая политика обязана срабатывать на своем нарушении и молчать на хорошем манифесте.
package main

import rego.v1

good_container := {
	"name": "app",
	"image": "app:1.2.3",
	"resources": {"requests": {"cpu": "100m", "memory": "64Mi"}, "limits": {"cpu": "1", "memory": "128Mi"}},
	"readinessProbe": {"httpGet": {"path": "/readyz", "port": 8080}},
	"livenessProbe": {"httpGet": {"path": "/healthz", "port": 8080}},
}

deployment(container, pod_spec) := {
	"kind": "Deployment",
	"metadata": {"name": "app"},
	"spec": {"template": {
		"metadata": {"labels": {"app": "app"}},
		"spec": object.union({"securityContext": {"runAsNonRoot": true}, "containers": [container]}, pod_spec),
	}},
}

service := {"kind": "Service", "metadata": {"name": "app"}, "spec": {"selector": {"app": "app"}}}

pdb := {"kind": "PodDisruptionBudget", "metadata": {"name": "app"}, "spec": {"minAvailable": 1, "selector": {"matchLabels": {"app": "app"}}}}

files(documents) := [{"path": "stdin", "contents": documents}]

test_good_manifest_passes if {
	count(deny) == 0 with input as files([deployment(good_container, {}), service, pdb])
}

test_latest_tag_denied if {
	c := object.union(good_container, {"image": "app:latest"})
	some msg in deny with input as files([deployment(c, {}), service, pdb])
	contains(msg, "без фиксированного тега")
}

test_missing_tag_denied if {
	c := object.union(good_container, {"image": "registry:5000/app"})
	some msg in deny with input as files([deployment(c, {}), service, pdb])
	contains(msg, "без фиксированного тега")
}

test_missing_limits_denied if {
	c := json.remove(good_container, ["/resources/limits"])
	msgs := deny with input as files([deployment(c, {}), service, pdb])
	msgs == {"app/app: не задан resources.limits.cpu", "app/app: не задан resources.limits.memory"}
}

test_missing_readiness_behind_service_denied if {
	c := object.remove(good_container, ["readinessProbe"])
	msgs := deny with input as files([deployment(c, {}), service, pdb])
	msgs == {"app/app: за Service, но без readinessProbe"}
}

test_worker_without_service_needs_no_probes if {
	c := object.remove(good_container, ["readinessProbe", "livenessProbe"])
	count(deny) == 0 with input as files([deployment(c, {}), pdb])
}

test_root_denied if {
	d := json.remove(deployment(good_container, {}), ["/spec/template/spec/securityContext"])
	msgs := deny with input as files([d, service, pdb])
	msgs == {"app/app: нет runAsNonRoot: true"}
}

test_host_network_denied if {
	msgs := deny with input as files([deployment(good_container, {"hostNetwork": true}), service, pdb])
	msgs == {"app: hostNetwork запрещен"}
}

test_missing_pdb_denied if {
	msgs := deny with input as files([deployment(good_container, {}), service])
	msgs == {"app: нет PodDisruptionBudget"}
}
