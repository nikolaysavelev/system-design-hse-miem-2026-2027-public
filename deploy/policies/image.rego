# Образ с тегом latest или без тега нельзя откатить и нельзя воспроизвести: под тем же именем завтра другой код.
package main

import rego.v1

deny contains msg if {
	some d in deployments
	some c in d.spec.template.spec.containers
	not pinned(c.image)
	msg := sprintf("%s/%s: образ %q без фиксированного тега (latest или тег не указан)", [d.metadata.name, c.name, c.image])
}

pinned(image) if {
	parts := split(image, ":")
	count(parts) > 1
	tag := parts[count(parts) - 1]
	tag != "latest"
	not contains(tag, "/") # "registry:5000/app" — это порт реестра, а не тег
}
