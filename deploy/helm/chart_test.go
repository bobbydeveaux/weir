// Package helm holds tests of the Helm chart in weir/. They render the chart
// with the helm program and read what comes out.
package helm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// render runs `helm template` on the chart with existingSecret set and the
// given --set values.
func render(t *testing.T, set ...string) (string, error) {
	t.Helper()
	return helmTemplate(t, "", append([]string{"existingSecret=weir"}, set...)...)
}

// helmTemplate runs `helm template` on the chart with a values file, if one is
// given, and the given --set values. Nothing is set for the caller.
func helmTemplate(t *testing.T, values string, set ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed, so the chart was not rendered")
	}
	args := []string{"template", "weir", "weir"}
	if values != "" {
		path := filepath.Join(t.TempDir(), "values.yaml")
		if err := os.WriteFile(path, []byte(values), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-f", path)
	}
	for _, s := range set {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	return string(out), err
}

// podSpecs splits a rendered chart into the documents that hold a pod: the
// server's Deployment and the migration's Job.
func podSpecs(out string) []string {
	var pods []string
	for _, doc := range strings.Split(out, "\n---\n") {
		if strings.Contains(doc, "\nkind: Deployment\n") || strings.Contains(doc, "\nkind: Job\n") {
			// The separator took the newline that ends the document.
			pods = append(pods, doc+"\n")
		}
	}
	return pods
}

func TestTheChartMountsTheDatabasesCertificateAuthorities(t *testing.T) {
	out, err := render(t, "databaseCA.configMap=weir-db-ca")
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	// The server and the migration both open the database, so both need the file.
	for _, want := range []string{
		"name: WEIR_DATABASE_SSLROOTCERT\n", "value: /etc/weir/db-ca/ca.pem",
		"mountPath: /etc/weir/db-ca",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered chart does not hold %q", want)
		}
	}
	if got := strings.Count(out, "name: weir-db-ca"); got != 2 {
		t.Errorf("the ConfigMap is a volume of %d pods; want 2: the server's and the migration's", got)
	}
	if got := strings.Count(out, "mountPath: /etc/weir/db-ca"); got != 2 {
		t.Errorf("the file is mounted in %d containers; want 2: the server and the migration", got)
	}

	// With the migration in the server's pod, it is mounted there too.
	out, err = render(t, "databaseCA.configMap=weir-db-ca", "migrate.when=start")
	if err != nil || strings.Count(out, "mountPath: /etc/weir/db-ca") != 2 {
		t.Errorf("with migrate.when=start the file is mounted %d times (%v); want 2", strings.Count(out, "mountPath: /etc/weir/db-ca"), err)
	}

	// Without the value, nothing is mounted and the setting is not made.
	out, err = render(t)
	if err != nil || strings.Contains(out, "db-ca") || strings.Contains(out, "WEIR_DATABASE_SSLROOTCERT") {
		t.Errorf("with no databaseCA the chart still mentions it (%v)", err)
	}
}

func TestTheChartRefusesIAMSignInWithAMigrationJob(t *testing.T) {
	out, err := render(t, "env.WEIR_DATABASE_IAM=aws")
	if err == nil || !strings.Contains(out, "Set migrate.when to start") {
		t.Errorf("rendering with IAM sign-in and a migration Job: err = %v\n%s\nwant a refusal that says to set migrate.when to start", err, out)
	}
	for _, ok := range [][]string{{"env.WEIR_DATABASE_IAM=aws", "migrate.when=start"}, {"env.WEIR_DATABASE_IAM=aws", "migrate.enabled=false"}} {
		if out, err := render(t, ok...); err != nil {
			t.Errorf("rendering with %v: %v\n%s", ok, err, out)
		}
	}
}

const csiSecretFiles = `
secretFiles:
  volume:
    csi:
      driver: secrets-store.csi.k8s.io
      readOnly: true
      volumeAttributes:
        secretProviderClass: weir
  adminKey: admin-key
`

func TestTheChartMountsSecretFilesFromACSIVolume(t *testing.T) {
	for _, when := range []string{"before", "start"} {
		out, err := helmTemplate(t, csiSecretFiles, "migrate.when="+when)
		if err != nil {
			t.Fatalf("migrate.when=%s: helm template: %v\n%s", when, err, out)
		}
		// With no existingSecret, nothing is taken from a Secret.
		if strings.Contains(out, "envFrom:") {
			t.Errorf("migrate.when=%s: envFrom is rendered with no existingSecret", when)
		}
		pods := podSpecs(out)
		wantPods := map[string]int{"before": 2, "start": 1}[when]
		if len(pods) != wantPods {
			t.Fatalf("migrate.when=%s: %d pods rendered; want %d", when, len(pods), wantPods)
		}
		for _, pod := range pods {
			for _, want := range []string{
				"- name: secret-files\n          csi:\n            driver: secrets-store.csi.k8s.io\n            readOnly: true\n            volumeAttributes:\n              secretProviderClass: weir\n",
				"- name: secret-files\n              mountPath: /etc/weir/secrets\n              readOnly: true\n",
				"- name: WEIR_SECRETS_DIR\n              value: \"/etc/weir/secrets\"\n",
				"- name: WEIR_ADMIN_KEY_FILE\n              value: \"/etc/weir/secrets/admin-key\"\n",
			} {
				if !strings.Contains(pod, want) {
					t.Errorf("migrate.when=%s: a pod does not hold %q:\n%s", when, want, pod)
				}
			}
		}
		// The server and the migration both mount it, whichever pod the
		// migration runs in.
		if got := strings.Count(out, "mountPath: /etc/weir/secrets"); got != 2 {
			t.Errorf("migrate.when=%s: the volume is mounted in %d containers; want 2", when, got)
		}
	}

	// A mount path of the user's own is used for the mount and both settings.
	out, err := helmTemplate(t, csiSecretFiles, "secretFiles.mountPath=/run/weir/")
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	if !strings.Contains(out, "mountPath: /run/weir/\n") || !strings.Contains(out, `value: "/run/weir/admin-key"`) {
		t.Errorf("with secretFiles.mountPath=/run/weir/ the mount or the admin key file is wrong:\n%s", out)
	}
}

func TestTheChartMountsSecretFilesFromASecretBesideTheDatabasesCertificateAuthorities(t *testing.T) {
	for _, when := range []string{"before", "start"} {
		out, err := render(t, "secretFiles.volume.secret.secretName=weir-files", "databaseCA.configMap=weir-db-ca", "migrate.when="+when)
		if err != nil {
			t.Fatalf("migrate.when=%s: helm template: %v\n%s", when, err, out)
		}
		for _, pod := range podSpecs(out) {
			// Two keys of the same name in one map would be invalid YAML, or
			// would quietly keep only the last of them.
			if got := strings.Count(pod, "volumes:"); got != 1 {
				t.Errorf("migrate.when=%s: a pod has %d volumes keys; want 1:\n%s", when, got, pod)
			}
			containers := strings.Count(pod, "args: [")
			if got := strings.Count(pod, "volumeMounts:"); got != containers {
				t.Errorf("migrate.when=%s: a pod's %d containers have %d volumeMounts keys; want one each:\n%s", when, containers, got, pod)
			}
			for _, want := range []string{
				"volumes:\n        - name: database-ca\n          configMap:\n            name: weir-db-ca\n        - name: secret-files\n          secret:\n            secretName: weir-files\n",
				"volumeMounts:\n            - name: database-ca\n              mountPath: /etc/weir/db-ca\n              readOnly: true\n            - name: secret-files\n              mountPath: /etc/weir/secrets\n              readOnly: true\n",
				"- name: WEIR_SECRETS_DIR\n",
			} {
				if !strings.Contains(pod, want) {
					t.Errorf("migrate.when=%s: a pod does not hold %q:\n%s", when, want, pod)
				}
			}
			// The admin key still comes from existingSecret.
			if !strings.Contains(pod, "secretRef:\n                name: weir\n") || strings.Contains(pod, "WEIR_ADMIN_KEY_FILE") {
				t.Errorf("migrate.when=%s: with existingSecret and no secretFiles.adminKey, the admin key is not left to the Secret:\n%s", when, pod)
			}
		}
	}
}

func TestTheChartIsUnchangedWithoutSecretFiles(t *testing.T) {
	out, err := render(t)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	if got := strings.Count(out, "secretRef:\n                name: weir\n"); got != 2 {
		t.Errorf("existingSecret is the environment of %d containers; want 2: the server and the migration", got)
	}
	for _, absent := range []string{"secret-files", "WEIR_SECRETS_DIR", "WEIR_ADMIN_KEY_FILE", "volumes:", "volumeMounts:"} {
		if strings.Contains(out, absent) {
			t.Errorf("with no secretFiles the chart still holds %q", absent)
		}
	}
}

func TestTheChartRefusesSecretFilesThatCannotWork(t *testing.T) {
	for _, c := range []struct {
		name, values string
		set          []string
		want         string
	}{
		{"an admin key with no volume", "", []string{"secretFiles.adminKey=admin-key"}, "Set secretFiles.volume"},
		{"an admin key with no volume, beside a Secret", "", []string{"existingSecret=weir", "secretFiles.adminKey=admin-key"}, "Set secretFiles.volume"},
		{"no admin key at all", "", nil, "Set existingSecret to a Secret that holds WEIR_ADMIN_KEY, or secretFiles.adminKey"},
		{"a volume with no admin key and no Secret", "", []string{"secretFiles.volume.secret.secretName=weir-files"}, "Set existingSecret to a Secret that holds WEIR_ADMIN_KEY, or secretFiles.adminKey"},
		{"the admin key in a workspace's directory", csiSecretFiles, []string{"secretFiles.adminKey=acme/admin-key"}, "must be a file at the top of the volume"},
	} {
		out, err := helmTemplate(t, c.values, c.set...)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s: err = %v\n%s\nwant a refusal that says %q", c.name, err, out, c.want)
		}
	}

	// A server set not to require an admin key may have none.
	if out, err := helmTemplate(t, "env:\n  WEIR_REQUIRE_ADMIN_KEY: \"\"\n"); err != nil {
		t.Errorf("with env.WEIR_REQUIRE_ADMIN_KEY empty and no admin key: %v\n%s", err, out)
	}
	// Setting it to anything else still requires one.
	if out, err := helmTemplate(t, "env:\n  WEIR_REQUIRE_ADMIN_KEY: \"1\"\n"); err == nil {
		t.Errorf("with env.WEIR_REQUIRE_ADMIN_KEY=1 and no admin key the chart rendered:\n%s", out)
	}
}
