// Package helm holds tests of the Helm chart in weir/. They render the chart
// with the helm program and read what comes out.
package helm

import (
	"os/exec"
	"strings"
	"testing"
)

// render runs `helm template` on the chart with the given --set values.
func render(t *testing.T, set ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed, so the chart was not rendered")
	}
	args := []string{"template", "weir", "weir", "--set", "existingSecret=weir"}
	for _, s := range set {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	return string(out), err
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
