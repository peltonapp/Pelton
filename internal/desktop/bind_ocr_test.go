package desktop

import "testing"

func TestOCRCode(t *testing.T) {
	cases := map[string]string{
		"deu.traineddata":        "deu",
		"chi_tra.traineddata.gz": "chi_tra",
		"Latin.TRAINEDDATA":      "Latin",
		"eng.txt":                "",
		"../x.traineddata":       "",
		"a.traineddata":          "",
		"de u.traineddata":       "",
		".traineddata":           "",
	}
	for name, want := range cases {
		if got := ocrCode(name); got != want {
			t.Errorf("ocrCode(%q) = %q, want %q", name, got, want)
		}
	}
}
