package discover_etcd_initial_cluster

import (
	"regexp"
	"testing"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
)

var (
	emptyInitialCluster = ""
	startedEtcdMember   = &etcdserverpb.Member{Name: "etcd-0", PeerURLs: []string{"https://etcd-0:2380"}}
	unstartedEtcdMember = &etcdserverpb.Member{Name: "", PeerURLs: []string{"https://etcd-0:2380"}}
	notFoundEtcdMember  = &etcdserverpb.Member{Name: "not-found", PeerURLs: []string{"https://not-found:2380"}}
)

func Test_ensureValidMember(t *testing.T) {
	tests := map[string]struct {
		member             *etcdserverpb.Member
		dataDirExists      bool
		wantMemberFound    bool
		wantInitialCluster string
		wantErr            bool
		wantErrString      string
	}{
		"started member found no dataDir": {
			member:             startedEtcdMember,
			wantMemberFound:    true,
			dataDirExists:      false,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "dataDir has been destroyed and must be removed from the cluster",
		},
		"started member found with dataDir": {
			member:             startedEtcdMember,
			wantMemberFound:    true,
			dataDirExists:      true,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            false,
		},
		"member not found with dataDir": {
			member:             notFoundEtcdMember,
			wantMemberFound:    false,
			dataDirExists:      true,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "check operator logs for possible scaling problems",
		},
		"member not found no dataDir": {
			member:             notFoundEtcdMember,
			wantMemberFound:    false,
			dataDirExists:      false,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "check operator logs for possible scaling problems",
		},
		"unstarted member found with dataDir": {
			member:             unstartedEtcdMember,
			wantMemberFound:    true,
			dataDirExists:      true,
			wantInitialCluster: emptyInitialCluster,
			wantErr:            true,
			wantErrString:      "previous members dataDir exists: archiving",
		},
		"unstarted member found no dataDir": {
			member:             unstartedEtcdMember,
			wantMemberFound:    true,
			dataDirExists:      false,
			wantInitialCluster: "etcd-0=https://etcd-0:2380",
			wantErr:            false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			o := DiscoverEtcdInitialClusterOptions{
				TargetPeerURLHost:   "etcd-0",
				TargetPeerURLScheme: "https",
				TargetPeerURLPort:   "2380",
				TargetName:          "etcd-0",
				DataDir:             "/tmp",
			}
			gotInitialCluster, gotMemberFound, err := o.getInitialCluster([]*etcdserverpb.Member{test.member}, test.dataDirExists)
			if gotInitialCluster != test.wantInitialCluster {
				t.Fatalf("initialCluster: want: %q, got: %q", test.wantInitialCluster, gotInitialCluster)
			}
			if err != nil && !test.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if err == nil && test.wantErr {
				t.Fatal("expected error got nil")
			}
			if gotMemberFound != test.wantMemberFound {
				t.Fatalf("memberFound: want %v, got %v", gotMemberFound, test.wantMemberFound)
			}
			if test.wantErrString != "" {
				regex := regexp.MustCompile(test.wantErrString)
				if len(regex.FindAll([]byte(err.Error()), -1)) != 1 {
					t.Fatalf("unexpected error wanted %q in %q", test.wantErrString, err.Error())
				}
			}
		})
	}

}
