package cliconfig

import (
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/aes"
	"github.com/docker/docker/pkg/homedir"
	"github.com/docker/engine-api/types"
)

func TestEmptyConfigDir(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	SetConfigDir(tmpHome)

	config, err := Load("")
	if err != nil {
		t.Fatalf("Failed loading on empty config dir: %q", err)
	}

	expectedConfigFilename := filepath.Join(tmpHome, ConfigFileName)
	if config.Filename() != expectedConfigFilename {
		t.Fatalf("Expected config filename %s, got %s", expectedConfigFilename, config.Filename())
	}

	// Now save it and make sure it shows up in new form
	saveConfigAndValidateNewFormat(t, config, tmpHome)
}

func TestMissingFile(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on missing file: %q", err)
	}

	// Now save it and make sure it shows up in new form
	saveConfigAndValidateNewFormat(t, config, tmpHome)
}

func TestSaveFileToDirs(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	tmpHome += "/.docker"

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on missing file: %q", err)
	}

	// Now save it and make sure it shows up in new form
	saveConfigAndValidateNewFormat(t, config, tmpHome)
}

func TestEmptyFile(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	fn := filepath.Join(tmpHome, ConfigFileName)
	if err := ioutil.WriteFile(fn, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	_, err = Load(tmpHome)
	if err == nil {
		t.Fatalf("Was supposed to fail")
	}
}

func TestEmptyJson(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	fn := filepath.Join(tmpHome, ConfigFileName)
	if err := ioutil.WriteFile(fn, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	// Now save it and make sure it shows up in new form
	saveConfigAndValidateNewFormat(t, config, tmpHome)
}

func TestOldInvalidsAuth(t *testing.T) {
	invalids := map[string]string{
		`username = test`: "The Auth config file is empty",
		`username
password`: "Invalid Auth config file",
		`username = test
email`: "Invalid auth configuration file",
	}

	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	homeKey := homedir.Key()
	homeVal := homedir.Get()

	defer func() { os.Setenv(homeKey, homeVal) }()
	os.Setenv(homeKey, tmpHome)

	for content, expectedError := range invalids {
		fn := filepath.Join(tmpHome, oldConfigfile)
		if err := ioutil.WriteFile(fn, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}

		config, err := Load(tmpHome)
		// Use Contains instead of == since the file name will change each time
		if err == nil || !strings.Contains(err.Error(), expectedError) {
			t.Fatalf("Should have failed\nConfig: %v\nGot: %v\nExpected: %v", config, err, expectedError)
		}

	}
}

func getAESKey(dir string) ([]byte, error) {
	if dir == "" {
		dir = homedir.Get()
	}
	aes.InitAESKey(dir)

	if aesKey, err := ioutil.ReadFile(filepath.Join(dir, ".docker/aeskey")); err != nil {
		return nil, err
	} else {
		return aesKey, nil
	}
}

func getEncryptedString(message string) (string, error) {
	aesKey, err := getAESKey("/root")
	if err != nil {
		return "", err
	}
	encryptedAuth, err := aes.AESEncrypt([]byte(message), aesKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encryptedAuth), nil
}

func TestOldValidAuth(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	homeKey := homedir.Key()
	homeVal := homedir.Get()

	defer func() { os.Setenv(homeKey, homeVal) }()
	os.Setenv(homeKey, tmpHome)

	fn := filepath.Join(tmpHome, oldConfigfile)
	js := `username = am9lam9lOmhlbGxv
	email = user@example.com`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatal(err)
	}

	// defaultIndexserver is https://index.docker.io/v1/
	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}

	// Now save it and make sure it shows up in new form
	configStr := saveConfigAndValidateNewFormat(t, config, tmpHome)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + string(encryptedAuthBase64) + `"
		}
	}
}`

	if configStr == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", configStr, expConfStr)
	}
}

func TestOldJsonInvalid(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	homeKey := homedir.Key()
	homeVal := homedir.Get()

	defer func() { os.Setenv(homeKey, homeVal) }()
	os.Setenv(homeKey, tmpHome)

	fn := filepath.Join(tmpHome, oldConfigfile)
	js := `{"https://index.docker.io/v1/":{"auth":"test","email":"user@example.com"}}`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	// Use Contains instead of == since the file name will change each time
	if err == nil || !strings.Contains(err.Error(), "Invalid auth configuration file") {
		t.Fatalf("Expected an error got : %v, %v", config, err)
	}
}

func TestOldJson(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	homeKey := homedir.Key()
	homeVal := homedir.Get()

	defer func() { os.Setenv(homeKey, homeVal) }()
	os.Setenv(homeKey, tmpHome)

	fn := filepath.Join(tmpHome, oldConfigfile)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := `{"https://index.docker.io/v1/":{"auth":"am9lam9lOmhlbGxv","email":"user@example.com"}}`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}

	// Now save it and make sure it shows up in new form
	configStr := saveConfigAndValidateNewFormat(t, config, tmpHome)

	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + encryptedAuthBase64 + `",
			"email": "user@example.com"
		}
	}
}`

	if configStr == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", configStr, expConfStr)
	}
}

func TestNewJson(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	fn := filepath.Join(tmpHome, ConfigFileName)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := ` { "auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `" } } }`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}

	// Now save it and make sure it shows up in new form
	configStr := saveConfigAndValidateNewFormat(t, config, tmpHome)

	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + encryptedAuthBase64 + `"
		}
	}
}`

	if configStr == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", configStr, expConfStr)
	}
}

func TestNewJsonNoEmail(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	fn := filepath.Join(tmpHome, ConfigFileName)
	js := ` { "auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `" } } }`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}

	// Now save it and make sure it shows up in new form
	configStr := saveConfigAndValidateNewFormat(t, config, tmpHome)

	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + encryptedAuthBase64 + `"
		}
	}
}`

	if configStr == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", configStr, expConfStr)
	}
}

func TestJsonWithPsFormat(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	fn := filepath.Join(tmpHome, ConfigFileName)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := `{
		"auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `", "email": "user@example.com" } },
		"psFormat": "table {{.ID}}\\t{{.Label \"com.docker.label.cpu\"}}"
}`
	if err := ioutil.WriteFile(fn, []byte(js), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(tmpHome)
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	if config.PsFormat != `table {{.ID}}\t{{.Label "com.docker.label.cpu"}}` {
		t.Fatalf("Unknown ps format: %s\n", config.PsFormat)
	}

	// Now save it and make sure it shows up in new form
	configStr := saveConfigAndValidateNewFormat(t, config, tmpHome)
	if !strings.Contains(configStr, `"psFormat":`) ||
		!strings.Contains(configStr, "{{.ID}}") {
		t.Fatalf("Should have save in new form: %s", configStr)
	}
}

// Save it and make sure it shows up in new form
func saveConfigAndValidateNewFormat(t *testing.T, config *ConfigFile, homeFolder string) string {
	if err := config.Save(); err != nil {
		t.Fatalf("Failed to save: %q", err)
	}

	buf, err := ioutil.ReadFile(filepath.Join(homeFolder, ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), `"auths":`) {
		t.Fatalf("Should have save in new form: %s", string(buf))
	}
	return string(buf)
}

func TestConfigDir(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	if ConfigDir() == tmpHome {
		t.Fatalf("Expected ConfigDir to be different than %s by default, but was the same", tmpHome)
	}

	// Update configDir
	SetConfigDir(tmpHome)

	if ConfigDir() != tmpHome {
		t.Fatalf("Expected ConfigDir to %s, but was %s", tmpHome, ConfigDir())
	}
}

func TestConfigFile(t *testing.T) {
	configFilename := "configFilename"
	configFile := NewConfigFile(configFilename)

	if configFile.Filename() != configFilename {
		t.Fatalf("Expected %s, got %s", configFilename, configFile.Filename())
	}
}

func TestJsonReaderNoFile(t *testing.T) {
	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := ` { "auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `", "email": "user@example.com" } } }`

	config, err := LoadFromReader(strings.NewReader(js))
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}
}

func TestOldJsonReaderNoFile(t *testing.T) {
	js := `{"https://index.docker.io/v1/":{"auth":"am9lam9lOmhlbGxv","email":"user@example.com"}}`

	config, err := LegacyLoadFromReader(strings.NewReader(js))
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	ac := config.AuthConfigs["https://index.docker.io/v1/"]
	if ac.Username != "joejoe" || ac.Password != "hello" {
		t.Fatalf("Missing data from parsing:\n%q", config)
	}
}

func TestJsonWithPsFormatNoFile(t *testing.T) {
	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := `{
		"auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `", "email": "user@example.com" } },
		"psFormat": "table {{.ID}}\\t{{.Label \"com.docker.label.cpu\"}}"
}`
	config, err := LoadFromReader(strings.NewReader(js))
	if err != nil {
		t.Fatalf("Failed loading on empty json file: %q", err)
	}

	if config.PsFormat != `table {{.ID}}\t{{.Label "com.docker.label.cpu"}}` {
		t.Fatalf("Unknown ps format: %s\n", config.PsFormat)
	}

}

func TestJsonSaveWithNoFile(t *testing.T) {
	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create a temp dir: %q", err)
	}
	defer os.RemoveAll(tmpHome)

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	js := `{
		"auths": { "https://index.docker.io/v1/": { "auth": "` + encryptedAuthBase64 + `" } },
		"psFormat": "table {{.ID}}\\t{{.Label \"com.docker.label.cpu\"}}"
}`
	config, err := LoadFromReader(strings.NewReader(js))
	if err != nil {
		t.Fatal(err)
	}
	err = config.Save()
	if err == nil {
		t.Fatalf("Expected error. File should not have been able to save with no file name.")
	}

	fn := filepath.Join(tmpHome, ConfigFileName)
	f, _ := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	defer f.Close()

	err = config.SaveToWriter(f)
	if err != nil {
		t.Fatalf("Failed saving to file: %q", err)
	}
	buf, err := ioutil.ReadFile(filepath.Join(tmpHome, ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + encryptedAuthBase64 + `"
		}
	},
	"psFormat": "table {{.ID}}\\t{{.Label \"com.docker.label.cpu\"}}"
}`
	if string(buf) == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", string(buf), expConfStr)
	}
}

func TestLegacyJsonSaveWithNoFile(t *testing.T) {

	js := `{"https://index.docker.io/v1/":{"auth":"am9lam9lOmhlbGxv","email":"user@example.com"}}`
	config, err := LegacyLoadFromReader(strings.NewReader(js))
	if err != nil {
		t.Fatal(err)
	}
	err = config.Save()
	if err == nil {
		t.Fatalf("Expected error. File should not have been able to save with no file name.")
	}

	tmpHome, err := ioutil.TempDir("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create a temp dir: %q", err)
	}
	defer os.RemoveAll(tmpHome)

	fn := filepath.Join(tmpHome, ConfigFileName)
	f, _ := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	defer f.Close()

	if err = config.SaveToWriter(f); err != nil {
		t.Fatalf("Failed saving to file: %q", err)
	}
	buf, err := ioutil.ReadFile(filepath.Join(tmpHome, ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}

	encryptedAuthBase64, err := getEncryptedString("am9lam9lOmhlbGxv")
	if err != nil {
		t.Fatal(err)
	}

	expConfStr := `{
	"auths": {
		"https://index.docker.io/v1/": {
			"auth": "` + encryptedAuthBase64 + `",
			"email": "user@example.com"
		}
	}
}`

	if string(buf) == expConfStr {
		t.Fatalf("Should not have the encrypted credential the same because of the random IV: \n%s\n should not \n%s", string(buf), expConfStr)
	}
}

func TestEncodeAuth(t *testing.T) {
	newAuthConfig := &types.AuthConfig{Username: "ken", Password: "test"}
	authStr := encodeAuth(newAuthConfig)
	decAuthConfig := &types.AuthConfig{}
	var err error
	decAuthConfig.Username, decAuthConfig.Password, err = decodeAuth(authStr)
	if err != nil {
		t.Fatal(err)
	}
	if newAuthConfig.Username != decAuthConfig.Username {
		t.Fatal("Encode Username doesn't match decoded Username")
	}
	if newAuthConfig.Password != decAuthConfig.Password {
		t.Fatal("Encode Password doesn't match decoded Password")
	}
	if authStr != "a2VuOnRlc3Q=" {
		t.Fatal("AuthString encoding isn't correct.")
	}
}
