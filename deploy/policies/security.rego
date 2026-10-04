# Контейнер не работает от root и не сидит в сети узла.
package main

import rego.v1

deny contains msg if {
	some d in deployments
	some c in d.spec.template.spec.containers
	not non_root(d, c)
	msg := sprintf("%s/%s: нет runAsNonRoot: true", [d.metadata.name, c.name])
}

non_root(d, _) if d.spec.template.spec.securityContext.runAsNonRoot == true

non_root(_, c) if c.securityContext.runAsNonRoot == true

deny contains msg if {
	some d in deployments
	d.spec.template.spec.hostNetwork == true
	msg := sprintf("%s: hostNetwork запрещен", [d.metadata.name])
}
