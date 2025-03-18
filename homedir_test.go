package homedir

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
)

// patchEnv modifies an environment variable for the duration of the test
func patchEnv(t testing.TB, key, value string) {
	t.Helper()
	original := os.Getenv(key)

	if value != "" {
		os.Setenv(key, value)
	} else {
		os.Unsetenv(key)
	}

	t.Cleanup(func() {
		if original != "" {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	})
}

// restoreCache ensures cache settings are restored after test
func restoreCache(t testing.TB) {
	t.Helper()
	origEnabled := CacheEnabled()

	t.Cleanup(func() {
		SetCacheEnable(origEnabled)
		Reset()
	})
}

func TestCacheControl(t *testing.T) {
	tests := []struct {
		name          string
		cacheEnabled  bool
		expectEnabled bool
	}{
		{"enable cache", true, true},
		{"disable cache", false, false},
	}

	origEnabled := CacheEnabled()
	defer SetCacheEnable(origEnabled)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetCacheEnable(tc.cacheEnabled)
			if CacheEnabled() != tc.expectEnabled {
				t.Errorf("expected cache enabled to be %v, got %v", tc.expectEnabled, CacheEnabled())
			}
		})
	}
}

func TestDir(t *testing.T) {
	restoreCache(t)

	// the goal of this test is to ensure that the Dir() function is honoring the cache settings,
	// responds to environment variable changes, and returns the expected home directory via the user package.

	u, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %s", err)
	}

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() failed: %s", err)
	}

	if u.HomeDir != dir {
		t.Errorf("expected home dir %q, got %q", u.HomeDir, dir)
	}

	// test caching behavior
	SetCacheEnable(true)
	Reset()

	// get dir the first time (should populate cache)
	expected, err := Dir()
	if err != nil {
		t.Fatalf("Dir() failed: %s", err)
	}

	// change HOME and verify we still get cached value
	homeEnv := "HOME"
	if runtime.GOOS == "plan9" {
		homeEnv = "home"
	} else if runtime.GOOS == "windows" {
		// try both HOME and USERPROFILE for Windows
		patchEnv(t, "USERPROFILE", "/invalid/profile")
	}

	patchEnv(t, homeEnv, "/invalid/path")

	cached, err := Dir()
	if err != nil {
		t.Fatalf("Dir() failed after env change: %s", err)
	}

	if cached != expected {
		t.Errorf("expected cached value %q, got %q", expected, cached)
	}

	// disable cache and verify we get the new value
	SetCacheEnable(false)

	// note: this might fail (return an unexpected value) on some platforms where HOME isn't the only variable
	// that's checked, which is fine - we're mainly testing the caching behavior
	cached, err = Dir()
	if err != nil {
		t.Fatalf("Dir() failed after disabling cache: %s", err)
	}

	if cached == expected {
		t.Errorf("expected uncached value %q, got %q", expected, cached)
	}
}

func TestDetectHomeDir(t *testing.T) {
	restoreCache(t)

	dir, err := detectHomeDir()
	if err != nil {
		t.Fatalf("detectHomeDir() failed: %s", err)
	}

	if dir == "" {
		t.Error("detectHomeDir() returned empty string")
	}
}

func TestDirWindows(t *testing.T) {
	restoreCache(t)

	tests := []struct {
		name        string
		env         map[string]string
		expected    string
		expectError bool
	}{
		{
			name: "home env var",
			env: map[string]string{
				"HOME":        "/windows/home",
				"USERPROFILE": "",
				"HOMEDRIVE":   "",
				"HOMEPATH":    "",
			},
			expected: "/windows/home",
		},
		{
			name: "userprofile env var",
			env: map[string]string{
				"HOME":        "",
				"USERPROFILE": "/windows/userprofile",
				"HOMEDRIVE":   "",
				"HOMEPATH":    "",
			},
			expected: "/windows/userprofile",
		},
		{
			name: "homedrive + homepath",
			env: map[string]string{
				"HOME":        "",
				"USERPROFILE": "",
				"HOMEDRIVE":   "C:",
				"HOMEPATH":    "\\windows\\drive",
			},
			expected: "C:\\windows\\drive",
		},
		{
			name: "no env vars set",
			env: map[string]string{
				"HOME":        "",
				"USERPROFILE": "",
				"HOMEDRIVE":   "",
				"HOMEPATH":    "",
			},
			expected:    "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				patchEnv(t, k, v)
			}

			dir, err := dirWindows()
			if tc.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if dir != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, dir)
				}
			}
		})
	}
}

func TestDirUnix(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		env         map[string]string
		expected    string
		expectError bool
		skipOnOS    string
	}{
		{
			name: "linux with HOME",
			goos: "linux",
			env: map[string]string{
				"HOME": "/unix/home",
			},
			expected:    "/unix/home",
			expectError: false,
		},
		{
			name: "darwin with HOME",
			goos: "darwin",
			env: map[string]string{
				"HOME": "/darwin/home",
			},
			expected:    "/darwin/home",
			expectError: false,
		},
		{
			name: "plan9 with home",
			goos: "plan9",
			env: map[string]string{
				"home": "/plan9/home",
			},
			expected:    "/plan9/home",
			expectError: false,
		},
		{
			name:        "empty HOME on unix",
			goos:        "linux",
			env:         map[string]string{"HOME": ""},
			expected:    "",
			expectError: false,     // not asserting error because fallbacks may work
			skipOnOS:    "windows", // skip on Windows
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnOS != "" && runtime.GOOS == tc.skipOnOS {
				t.Skipf("skipping %s on %s", tc.name, tc.skipOnOS)
			}

			for k, v := range tc.env {
				patchEnv(t, k, v)
			}

			dir, err := dirUnix(tc.goos)

			if tc.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			// if we don't expect error but got one, it might be ok for some cases
			// where we're not testing error conditions explicitly
			if err != nil && tc.expected != "" {
				t.Fatalf("unexpected error: %s", err)
			}

			// only verify the output if we have an expected value and no error
			if tc.expected != "" && err == nil {
				if dir != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, dir)
				}
			}
		})
	}
}

func TestExpand(t *testing.T) {
	restoreCache(t)

	u, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %s", err)
	}

	tests := []struct {
		name   string
		input  string
		output string
		err    bool
	}{
		{
			name:   "non-tilde path",
			input:  "/foo",
			output: "/foo",
			err:    false,
		},
		{
			name:   "tilde with path",
			input:  "~/foo",
			output: filepath.Join(u.HomeDir, "foo"),
			err:    false,
		},
		{
			name:   "empty path",
			input:  "",
			output: "",
			err:    false,
		},
		{
			name:   "tilde only",
			input:  "~",
			output: u.HomeDir,
			err:    false,
		},
		{
			name:   "tilde with user",
			input:  "~user/foo",
			output: "",
			err:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Expand(tc.input)
			if (err != nil) != tc.err {
				t.Fatalf("Expand(%q) error: got %v, want error: %v", tc.input, err, tc.err)
			}

			if actual != tc.output {
				t.Errorf("Expand(%q) = %q, want %q", tc.input, actual, tc.output)
			}
		})
	}

	// test with cache disabled and custom home
	t.Run("custom home with disabled cache", func(t *testing.T) {
		SetCacheEnable(false)
		homeEnv := "HOME"
		if runtime.GOOS == "windows" {
			homeEnv = "USERPROFILE" // more likely to work on Windows
		}

		patchEnv(t, homeEnv, "/custom/path")

		// reset cache to ensure we pick up the new home
		Reset()

		expected := filepath.Join("/custom/path", "foo/bar")
		actual, err := Expand("~/foo/bar")

		if err != nil {
			t.Errorf("no error expected, got: %v", err)
		} else if actual != expected {
			t.Errorf("expected: %v; actual: %v", expected, actual)
		}
	})
}

func BenchmarkDir(b *testing.B) {
	restoreCache(b)

	tests := []struct {
		name     string
		useCache bool
	}{
		{"cached", true},
		{"uncached", false},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			SetCacheEnable(tc.useCache)

			if tc.useCache {
				Reset() // start with empty cache

				// warmup
				_, err := Dir()
				if err != nil {
					b.Fatal("warmup failed:", err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := Dir()
				if err != nil {
					b.Fatal("Dir() failed:", err)
				}
			}
		})
	}
}
