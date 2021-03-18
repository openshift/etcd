package discover_etcd_initial_cluster

import (
	"bytes"
	"context"
	"fmt"
	"go.etcd.io/etcd/integration"
	"go.etcd.io/etcd/pkg/transport"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var testTLSInfo = transport.TLSInfo{
	KeyFile:        "../../../integration/fixtures/server.key.insecure",
	CertFile:       "../../../integration/fixtures/server.crt",
	TrustedCAFile:  "../../../integration/fixtures/ca.crt",
	ClientCertAuth: true,
}

var (
	targetHostNotFound  = "[::]"
	emptyInitialCluster = []string{""}
	invalidEndpoint     = []string{"http://127.0.0.1:1234"}
	allActiveEndpoints  []string
	unstartedMemberHost = "0.0.0.0"
	unstartedMemberName = "unstarted-member"
	noDataDir           = ""
)

func TestDiscoverInitialCluster(t *testing.T) {
	clus := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size:               3,
		PeerTLS:            &testTLSInfo,
		ClientTLS:          &testTLSInfo,
		SkipCreatingClient: false,
	})
	defer clus.Terminate(t)
	// second cluster is setup just to destroy the dataDir in destructive test.
	clus2 := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size:               1,
		SkipCreatingClient: true,
	})
	defer clus2.Terminate(t)

	allActiveEndpoints = []string{clus.Members[0].GRPCAddr(), clus.Members[1].GRPCAddr(), clus.Members[2].GRPCAddr()}

	target, err := url.Parse(clus.Members[0].PeerURLs.String())
	if err != nil {
		t.Fatal(err)
	}
	targetHost, targetPort, err := net.SplitHostPort(target.Host)
	if err != nil {
		t.Fatal(err)
	}
	client := clus.RandClient()

	// add unstarted member
	if _, err := client.MemberAdd(context.TODO(), []string{fmt.Sprintf("unixs://0.0.0.0:%s", targetPort)}); err != nil {
		t.Fatal(err)
	}

	cluster, err := client.MemberList(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	var wantInitialCluster []string
	for _, member := range cluster.Members {
		// generate initial cluster <name>=<peerURL>
		var memberName string
		if member.Name == "" {
			memberName = "unstarted-member"
		} else {
			memberName = member.Name
		}
		wantInitialCluster = append(wantInitialCluster, fmt.Sprintf("%s=%s", memberName, member.PeerURLs[0]))
	}

	tests := map[string]struct {
		targetName         string
		targetHost         string
		endpoints          []string
		dataDir            string
		wantInitialCluster []string
		wantErr            bool
		wantErrString      string
	}{
		"started member found no dataDir": {
			targetName:         clus.Members[0].Name,
			targetHost:         targetHost,
			endpoints:          allActiveEndpoints,
			dataDir:            noDataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      fmt.Sprintf("member \"unixs://127.0.0.1:%s\" dataDir has been destoyed and must be removed from the cluster", targetPort),
		},
		"started member found with dataDir": {
			targetName:         clus.Members[0].Name,
			targetHost:         targetHost,
			endpoints:          allActiveEndpoints,
			dataDir:            clus.Members[0].DataDir,
			wantInitialCluster: emptyInitialCluster,
		},
		"create client fail no dataDir": {
			targetName:         clus.Members[0].Name,
			targetHost:         targetHost,
			endpoints:          invalidEndpoint,
			dataDir:            noDataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "failed to create etcd client: context deadline exceeded",
		},
		"create client fail with dataDir": {
			targetName:         clus.Members[0].Name,
			targetHost:         targetHost,
			endpoints:          invalidEndpoint,
			dataDir:            clus.Members[0].DataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            false,
		},
		"member not found with dataDir": {
			targetName:         "not-a-member",
			targetHost:         targetHostNotFound,
			endpoints:          allActiveEndpoints,
			dataDir:            clus.Members[0].DataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      fmt.Sprintf("member \"unixs://%s:%s\" is no longer a member of the cluster and should not start", targetHostNotFound, targetPort),
		},
		"member not found no dataDir": {
			targetName:         "not-a-member",
			targetHost:         targetHostNotFound,
			endpoints:          allActiveEndpoints,
			dataDir:            noDataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "timed out",
		},
		"unstarted member found with dataDir": { //destructive
			targetName:         unstartedMemberName,
			targetHost:         unstartedMemberHost,
			endpoints:          allActiveEndpoints,
			dataDir:            clus2.Members[0].DataDir,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      fmt.Sprintf("member \"unixs://%s:%s\" is unstarted but previous members dataDir exists: archiving to \"/tmp-removed-archive\"", unstartedMemberHost, targetPort),
		},
		"unstarted member found no dataDir": {
			targetName:         unstartedMemberName,
			targetHost:         unstartedMemberHost,
			endpoints:          allActiveEndpoints,
			dataDir:            noDataDir,
			wantInitialCluster: wantInitialCluster,
			wantErr:            false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			o := DiscoverEtcdInitialClusterOptions{
				TargetPeerURLHost:   test.targetHost,
				TargetPeerURLScheme: "unixs",
				TargetPeerURLPort:   targetPort,
				TargetName:          test.targetName,
				CABundleFile:        testTLSInfo.TrustedCAFile,
				ClientCertFile:      testTLSInfo.CertFile,
				ClientKeyFile:       testTLSInfo.KeyFile,
				Endpoints:           test.endpoints,
				DataDir:             test.dataDir,
			}

			if err := checkInitialClusterOutput(o, test.wantInitialCluster, test.wantErr, test.wantErrString); err != nil {
				t.Error(err)
			}
		})
	}
}

func checkInitialClusterOutput(o DiscoverEtcdInitialClusterOptions, wantInitialCluster []string, wantErr bool, wantErrString string) error {
	// capture stdout
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() {
		os.Stdout = stdout
	}()
	o.Validate()
	// check for expected errors
	outErr := o.Run()
	if outErr != nil && !wantErr {
		return fmt.Errorf("unexpected error %v", outErr)
	}
	if outErr == nil && wantErr {
		return fmt.Errorf("expected error")
	}
	// validate error string is as expected
	if wantErr && wantErrString != outErr.Error() {
		return fmt.Errorf("expected error: %q got: %q", wantErrString, outErr)
	}
	w.Close()
	var out bytes.Buffer
	io.Copy(&out, r)

	// validate output
	gotInitialCluster := strings.Split(out.String(), ",")
	// etcd will fail if we print a value that is not in a <key>=<value> format
	if len(gotInitialCluster) == 1 && len(gotInitialCluster[0]) > 0 {
		return fmt.Errorf("expected <key>=<value> pairs got: %q", gotInitialCluster)
	}
	// not concerned about order for this test
	sort.Strings(gotInitialCluster)
	sort.Strings(wantInitialCluster)
	if !reflect.DeepEqual(gotInitialCluster, wantInitialCluster) {
		return fmt.Errorf("expected: %q got: %q", wantInitialCluster, gotInitialCluster)
	}
	return nil
}
