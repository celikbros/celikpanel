package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type buildGateOperation struct {
	name string
	run  func(expectedBuildCommit string) string
}

func protectedBuildGateOperations() []buildGateOperation {
	agent := &Agent{}
	return []buildGateOperation{
		{
			name: "ApplyVhost",
			run: func(expected string) string {
				var response ApplyVhostResponse
				_ = agent.ApplyVhost(&ApplyVhostRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "ApplyVhosts",
			run: func(expected string) string {
				var response ApplyVhostsResponse
				_ = agent.ApplyVhosts(&ApplyVhostsRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "CreateSite",
			run: func(expected string) string {
				var response transport.CreateSiteResponse
				_ = agent.CreateSite(transport.CreateSiteRequest{ExpectedBuildCommit: expected}, &response)
				return response.ErrorMessage
			},
		},
		{
			name: "DeleteCertLineage",
			run: func(expected string) string {
				var response DeleteCertLineageResponse
				_ = agent.DeleteCertLineage(&DeleteCertLineageRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "DeleteSite",
			run: func(expected string) string {
				var response DeleteSiteResponse
				_ = agent.DeleteSite(&DeleteSiteRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "ExtractCpmoveFiles",
			run: func(expected string) string {
				var response CpmoveExtractResponse
				err := agent.ExtractCpmoveFiles(
					&CpmoveExtractRequest{ExpectedBuildCommit: expected},
					&response,
				)
				if err != nil {
					return err.Error()
				}
				return response.Error
			},
		},
		{
			name: "ImportCpmoveDatabase",
			run: func(expected string) string {
				var response CpmoveImportDBResponse
				err := agent.ImportCpmoveDatabase(
					&CpmoveImportDBRequest{ExpectedBuildCommit: expected},
					&response,
				)
				if err != nil {
					return err.Error()
				}
				return response.Error
			},
		},
		{
			name: "InspectCpmove",
			run: func(expected string) string {
				var response CpmoveInspectResponse
				err := agent.InspectCpmove(
					&CpmoveInspectRequest{ExpectedBuildCommit: expected},
					&response,
				)
				if err != nil {
					return err.Error()
				}
				return response.Error
			},
		},
		{
			name: "InstallCustomCertificate",
			run: func(expected string) string {
				var response InstallCertResponse
				_ = agent.InstallCustomCertificate(InstallCertRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "InstallWordPress",
			run: func(expected string) string {
				var response InstallWordPressResponse
				_ = agent.InstallWordPress(&InstallWordPressRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "IssueLetsEncryptCertificate",
			run: func(expected string) string {
				var response IssueLetsEncryptResponse
				_ = agent.IssueLetsEncryptCertificate(IssueLetsEncryptRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "IssuePanelCertificate",
			run: func(expected string) string {
				var response IssuePanelCertResponse
				_ = agent.IssuePanelCertificate(&IssuePanelCertRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "ReconcileSiteCertLineages",
			run: func(expected string) string {
				var response ReconcileSiteCertLineagesResponse
				_ = agent.ReconcileSiteCertLineages(
					&ReconcileSiteCertLineagesRequest{ExpectedBuildCommit: expected},
					&response,
				)
				return response.Error
			},
		},
		{
			name: "RenewLetsEncryptCertificate",
			run: func(expected string) string {
				var response RenewCertResponse
				_ = agent.RenewLetsEncryptCertificate(RenewCertRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
		{
			name: "RestartPanelSoon",
			run: func(expected string) string {
				var response bool
				err := agent.RestartPanelSoon(
					&RestartPanelSoonRequest{ExpectedBuildCommit: expected},
					&response,
				)
				if err == nil {
					return ""
				}
				return err.Error()
			},
		},
		{
			name: "SecureMailTLS",
			run: func(expected string) string {
				var response SecureMailTLSResponse
				_ = agent.SecureMailTLS(&SecureMailTLSRequest{ExpectedBuildCommit: expected}, &response)
				return response.Error
			},
		},
	}
}

func agentRPCMethodsWithBuildCommitField(t *testing.T) []string {
	t.Helper()

	agentType := reflect.TypeOf(&Agent{})
	methods := make([]string, 0)
	for index := 0; index < agentType.NumMethod(); index++ {
		method := agentType.Method(index)
		if method.Type.NumIn() != 3 {
			continue
		}
		requestType := method.Type.In(1)
		if requestType.Kind() == reflect.Pointer {
			requestType = requestType.Elem()
		}
		if requestType.Kind() != reflect.Struct {
			continue
		}
		field, ok := requestType.FieldByName("ExpectedBuildCommit")
		if !ok {
			continue
		}
		if field.Type.Kind() != reflect.String {
			t.Fatalf("%s ExpectedBuildCommit type = %s, want string", method.Name, field.Type)
		}
		methods = append(methods, method.Name)
	}
	sort.Strings(methods)
	return methods
}

func TestProtectedBuildCommitOperationMatrixIsComplete(t *testing.T) {
	operations := protectedBuildGateOperations()
	want := make([]string, 0, len(operations))
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if _, exists := seen[operation.name]; exists {
			t.Fatalf("duplicate protected operation %q", operation.name)
		}
		seen[operation.name] = struct{}{}
		want = append(want, operation.name)
	}
	sort.Strings(want)

	got := agentRPCMethodsWithBuildCommitField(t)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"build-commit RPC coverage changed\ncovered: %v\ndeclared: %v\n"+
				"add every declared operation to the behavioral gate matrix",
			want,
			got,
		)
	}
}

func TestProtectedBuildCommitOperationsFailClosedInProduction(t *testing.T) {
	previousCommit := buildCommit
	buildCommit = "agent-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	for _, operation := range protectedBuildGateOperations() {
		for _, scenario := range []struct {
			name     string
			expected string
			want     string
		}{
			{name: "missing panel identity", want: "expected panel build commit"},
			{name: "mismatched panel identity", expected: "different-panel-release", want: "panel/agent build mismatch"},
		} {
			t.Run(operation.name+"/"+scenario.name, func(t *testing.T) {
				got := operation.run(scenario.expected)
				if !strings.Contains(got, scenario.want) {
					t.Fatalf("gate error = %q, want substring %q", got, scenario.want)
				}
			})
		}
	}
}
