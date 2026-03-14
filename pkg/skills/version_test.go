package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	testcases := []struct {
		name     string
		input    string
		expected []int
		wantErr  bool
	}{
		{"simple", "1.2.3", []int{1, 2, 3}, false},
		{"v-prefix", "v1.2.3", []int{1, 2, 3}, false},
		{"two-part", "1.0", []int{1, 0}, false},
		{"single", "5", []int{5}, false},
		{"pre-release stripped", "2.1.0-beta.1", []int{2, 1, 0}, false},
		{"with spaces", "  v3.0.1  ", []int{3, 0, 1}, false},
		{"empty", "", nil, true},
		{"letters", "abc", nil, true},
		{"mixed", "1.x.3", nil, true},
		{"negative", "1.-2.3", nil, true},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseVersion(tc.input)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	testcases := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"a newer major", "2.0.0", "1.9.9", 1},
		{"b newer minor", "1.2.3", "1.3.0", -1},
		{"a newer patch", "1.2.4", "1.2.3", 1},
		{"missing patch treated as zero", "1.2", "1.2.0", 0},
		{"v-prefix", "v1.0.0", "1.0.0", 0},
		{"pre-release ignored", "1.0.1-beta", "1.0.1", 0},
		{"both unparseable", "abc", "xyz", 0},
		{"a unparseable", "abc", "1.0.0", -1},
		{"b unparseable", "1.0.0", "xyz", 1},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, CompareVersions(tc.a, tc.b))
		})
	}
}

func TestVersionIsNewer(t *testing.T) {
	assert.True(t, VersionIsNewer("2.0.0", "1.0.0"))
	assert.True(t, VersionIsNewer("1.1.0", "1.0.9"))
	assert.False(t, VersionIsNewer("1.0.0", "1.0.0"))
	assert.False(t, VersionIsNewer("0.9.0", "1.0.0"))
}
