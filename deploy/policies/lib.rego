# Общие выборки. conftest запускается с --combine: input — список файлов, в каждом один или несколько документов.
package main

import rego.v1

docs contains d if {
	some f in input
	is_object(f.contents)
	d := f.contents
}

docs contains d if {
	some f in input
	is_array(f.contents)
	some d in f.contents
}

deployments contains d if {
	some d in docs
	d.kind == "Deployment"
}

# метки селектора входят в метки пода
selects(selector, labels) if {
	count(selector) > 0
	every k, v in selector {
		labels[k] == v
	}
}
