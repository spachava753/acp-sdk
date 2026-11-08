package typegen

import "github.com/google/jsonschema-go/jsonschema"

// Keep discriminator references consistent with synthetic field reservations,
// including references emitted outside the union declaration itself.
func unionDiscriminatorFieldName(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema, discriminator string) string {
	var reserved []string
	for _, branch := range unionBranches(schema) {
		if explicitExtraFields(branch) {
			reserved = append(reserved, "ExtraFields")
			break
		}
	}
	for name, prop := range variantProperties(defs, schema) {
		if name != discriminator && prop.Const == nil && preservesJSONNull(defs, name, prop) {
			reserved = append(reserved, "NullFields")
			break
		}
	}
	return uniqueFieldNamesWithReserved([]string{discriminator}, reserved)[discriminator]
}
