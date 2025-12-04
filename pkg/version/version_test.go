package version

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"1.0.0-beta", "1.0.0", 0}, // 忽略后缀
		{"dev", "1.0.0", -1},
		{"1.0.0", "dev", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.3.4", "1.2.3", 0}, // 只比较前三个部分
	}

	for _, tt := range tests {
		result := Compare(tt.v1, tt.v2)
		if result != tt.expected {
			t.Errorf("Compare(%s, %s) = %d, expected %d", tt.v1, tt.v2, result, tt.expected)
		}
	}
}

func TestNeedUpdate(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.1", "1.0.0", false},
		{"dev", "1.0.0", true},
	}

	for _, tt := range tests {
		result := NeedUpdate(tt.current, tt.latest)
		if result != tt.expected {
			t.Errorf("NeedUpdate(%s, %s) = %v, expected %v", tt.current, tt.latest, result, tt.expected)
		}
	}
}

func TestExtractNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"123", 123},
		{"123-beta", 123},
		{"0", 0},
		{"1rc1", 1},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		result := extractNumber(tt.input)
		if result != tt.expected {
			t.Errorf("extractNumber(%s) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

