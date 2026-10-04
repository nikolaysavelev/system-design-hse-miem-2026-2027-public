# У каждого Deployment есть PodDisruptionBudget: обслуживание узла не уводит все реплики сразу.
package main

import rego.v1

deny contains msg if {
	some d in deployments
	not has_pdb(d)
	msg := sprintf("%s: нет PodDisruptionBudget", [d.metadata.name])
}

has_pdb(d) if {
	some p in docs
	p.kind == "PodDisruptionBudget"
	selects(p.spec.selector.matchLabels, d.spec.template.metadata.labels)
}
