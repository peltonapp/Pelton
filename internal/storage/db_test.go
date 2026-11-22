package storage

import "testing"

func TestDataDirName(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		dev     bool
		want    string
	}{
		{name: "stable", want: appDirName},
		{name: "nightly gets its own directory", channel: ChannelNightly, want: appDirName + "-nightly"},
		{name: "dev run", dev: true, want: appDirName + "-dev"},
		// a dev run of a nightly binary must stay on the throwaway dev data
		// rather than the nightly's own.
		{name: "dev wins over channel", channel: ChannelNightly, dev: true, want: appDirName + "-dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dev {
				t.Setenv("PELTON_DEV", "1")
			} else {
				t.Setenv("PELTON_DEV", "")
			}
			if got := DataDirName(tt.channel); got != tt.want {
				t.Errorf("DataDirName(%q) = %q, want %q", tt.channel, got, tt.want)
			}
		})
	}
}
