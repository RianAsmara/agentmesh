package domain

import "regexp"

// slugPattern membatasi identifier yang muncul di URL: huruf kecil, angka,
// dan tanda hubung di tengah. Tidak boleh diawali atau diakhiri tanda
// hubung, tidak boleh underscore, tidak boleh spasi. Aturan ini sama
// dengan yang dipakai DNS label dan Kubernetes resource name, sehingga
// nama yang valid di sini aman dipakai sebagai hostname nanti.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// versionPattern membatasi versi kapabilitas ke bentuk v1, v2, v10.
var versionPattern = regexp.MustCompile(`^v[0-9]+$`)

// validateSlug memeriksa panjang dan bentuk sebuah identifier URL-safe.
// Parameter field dipakai agar pesan error menyebut nama field yang tepat
// ("slug" untuk tenant, "name" untuk agent) meski aturannya sama.
func validateSlug(field, value string, minLen, maxLen int) error {
	if len(value) < minLen || len(value) > maxLen {
		return invalidField(field, "must be between "+itoa(minLen)+" and "+itoa(maxLen)+" characters")
	}
	if !slugPattern.MatchString(value) {
		return invalidField(field, "must contain only lowercase letters, digits, and internal hyphens")
	}
	return nil
}

// itoa membungkus strconv.Itoa agar import di file ini tetap minimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
