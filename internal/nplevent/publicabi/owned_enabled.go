//go:build c6_owned_entry

package publicabi

// Link only the explicitly selected guarded runtime-owner frame provider.
import _ "github.com/hilli/esp32c6-radio/internal/nplevent/owned"
