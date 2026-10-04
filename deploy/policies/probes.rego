# Deployment, на который смотрит Service, получает трафик: без readiness запросы попадут в под, который еще
# не готов, без liveness зависший процесс останется в ротации.
package main

import rego.v1

deny contains msg if {
	some d in deployments
	behind_service(d)
	some c in d.spec.template.spec.containers
	some probe in ["readinessProbe", "livenessProbe"]
	not c[probe]
	msg := sprintf("%s/%s: за Service, но без %s", [d.metadata.name, c.name, probe])
}

behind_service(d) if {
	some s in docs
	s.kind == "Service"
	selects(s.spec.selector, d.spec.template.metadata.labels)
}
