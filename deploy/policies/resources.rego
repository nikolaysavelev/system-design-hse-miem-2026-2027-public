# Без limits один под забирает узел целиком; без requests планировщик и HPA не знают, сколько ему нужно.
package main

import rego.v1

deny contains msg if {
	some d in deployments
	some c in d.spec.template.spec.containers
	some kind in ["requests", "limits"]
	some res in ["cpu", "memory"]
	not c.resources[kind][res]
	msg := sprintf("%s/%s: не задан resources.%s.%s", [d.metadata.name, c.name, kind, res])
}
