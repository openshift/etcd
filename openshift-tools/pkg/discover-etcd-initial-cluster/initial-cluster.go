package discover_etcd_initial_cluster

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"google.golang.org/grpc"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	"go.etcd.io/etcd/client/v3"
)

type DiscoverEtcdInitialClusterOptions struct {
	// TargetPeerURLHost is the host portion of the peer URL.  It is used to match on. (either IP or hostname)
	TargetPeerURLHost string
	// TargetPeerURLScheme is the host scheme of the peer URL.
	TargetPeerURLScheme string
	// TargetPeerURLPort is the host port of the peer URL.
	TargetPeerURLPort string
	// TargetName is the name to assign to this peer if we create it.
	TargetName string

	// CABundleFile is the file to use to trust the etcd server
	CABundleFile string
	// ClientCertFile is the client cert to use to authenticate this binary to etcd
	ClientCertFile string
	// ClientKeyFile is the client key to use to authenticate this binary to etcd
	ClientKeyFile string
	// Endpoints is a list of all the endpoints to use to try to contact etcd
	Endpoints []string

	// DataDir is the directory created when etcd starts the first time
	DataDir string
}

func NewDiscoverEtcdInitialCluster() *DiscoverEtcdInitialClusterOptions {
	return &DiscoverEtcdInitialClusterOptions{
		TargetPeerURLScheme: "https",
		TargetPeerURLPort:   "2380",
	}
}

func NewDiscoverEtcdInitialClusterCommand() *cobra.Command {
	o := NewDiscoverEtcdInitialCluster()

	cmd := &cobra.Command{
		Use:   "discover-etcd-initial-cluster",
		Short: "output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod",
		Long: `output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod

Please see docs for more details:
https://github.com/openshift/cluster-etcd-operator/tree/master/docs/discover-etcd-initial-cluster.md
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.Validate(); err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(1)
			}

			if err := o.Run(); err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
	o.BindFlags(cmd.Flags())

	return cmd
}

func (o *DiscoverEtcdInitialClusterOptions) BindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.CABundleFile, "cacert", o.CABundleFile, "file to use to verify the identity of the etcd server")
	flags.StringVar(&o.ClientCertFile, "cert", o.ClientCertFile, "client cert to use to authenticate this binary to etcd")
	flags.StringVar(&o.ClientKeyFile, "key", o.ClientKeyFile, "client key to use to authenticate this binary to etcd")
	flags.StringSliceVar(&o.Endpoints, "endpoints", o.Endpoints, "list of all the endpoints to use to try to contact etcd")
	flags.StringVar(&o.DataDir, "data-dir", o.DataDir, "dir to stat for existence of the member directory")
	flags.StringVar(&o.TargetPeerURLHost, "target-peer-url-host", o.TargetPeerURLHost, "host portion of the peer URL.  It is used to match on. (either IP or hostname)")
	flags.StringVar(&o.TargetName, "target-name", o.TargetName, "name to assign to this peer if we create it")
}

func (o *DiscoverEtcdInitialClusterOptions) Validate() error {
	if len(o.CABundleFile) == 0 {
		return fmt.Errorf("missing --cacert")
	}
	if len(o.ClientCertFile) == 0 {
		return fmt.Errorf("missing --cert")
	}
	if len(o.ClientKeyFile) == 0 {
		return fmt.Errorf("missing --key")
	}
	if len(o.Endpoints) == 0 {
		return fmt.Errorf("missing --endpoints")
	}
	if len(o.DataDir) == 0 {
		return fmt.Errorf("missing --data-dir")
	}
	if len(o.TargetPeerURLHost) == 0 {
		return fmt.Errorf("missing --target-peer-url-host")
	}
	if len(o.TargetName) == 0 {
		return fmt.Errorf("missing --target-name")
	}
	if len(o.TargetPeerURLPort) == 0 {
		return fmt.Errorf("missing TargetPeerURLPort")
	}
	if len(o.TargetPeerURLScheme) == 0 {
		return fmt.Errorf("missing TargetPeerURLScheme")
	}
	return nil
}

func (o *DiscoverEtcdInitialClusterOptions) Run() error {
	var dataDirExists bool
	// check if dataDir structure exists
	_, err := os.Stat(filepath.Join(o.DataDir, "member/snap"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		fmt.Fprintf(os.Stderr, "dataDir is present on %s\n", o.TargetName)
		dataDirExists = true
	}

	client, err := o.getClient()

	// Condition: create client fail with dataDir
	// Possible reasons for this condition.
	// 1.) single node etcd cluster
	// 2.) transient networking problem
	// 3.) on and off flow
	// Result: start etcd with empty initial config
	if err != nil && dataDirExists {
		fmt.Fprintf(os.Stderr, "failed to create etcd client, but the server is already initialized as member %q before, starting as etcd member: %v", o.TargetName, err.Error())
		return nil
	}
	// Condition: create client fail, no dataDir
	// Possible reasons for the condition include transient network partition.
	// Result: return error and restart container
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %v", err)
	}
	defer client.Close()

	for i := 0; i < 10; i++ {
		fmt.Fprintf(os.Stderr, "#### attempt %d\n", i)

		// Check member list on each iteration for changes.
		cluster, err := client.Cluster.(clientv3.NonLinearizeableMemberLister).NonLinearizeableMemberList(context.TODO())
		if err != nil {
			fmt.Fprintf(os.Stderr, "member list request failed: %v", err)
			continue
		}
		logCurrentMembership(cluster.Members)

		initialCluster, memberFound, err := o.getInitialCluster(cluster.Members, dataDirExists)
		if err != nil && memberFound {
			return err
		}
		// If member is not yet part of the cluster print to stderr and retry.
		if err != nil && !memberFound {
			fmt.Fprintf(os.Stderr, "      %s\n#### sleeping...\n", err.Error())
			time.Sleep(1 * time.Second)
			continue
		}
		// Empty string value for initialCluster is valid.
		fmt.Println(initialCluster)

		return nil
	}
	return fmt.Errorf("timed out")
}

func (o *DiscoverEtcdInitialClusterOptions) getInitialCluster(members []*etcdserverpb.Member, dataDirExists bool) (string, bool, error) {
	target := url.URL{
		Scheme: o.TargetPeerURLScheme,
		Host:   fmt.Sprintf("%s:%s", o.TargetPeerURLHost, o.TargetPeerURLPort),
	}

	targetMember, memberFound := checkTargetMember(target, members)

	// Condition: unstarted member found, no dataDir
	// This member is part of the cluster but has not yet started. We know this because the name is populated at
	// runtime which this member does not have.
	// Result: populate initial cluster so etcd can communicate with peers during startup
	if memberFound && targetMember.Name == "" && !dataDirExists {
		return formatInitialCluster(o.TargetName, targetMember, members), memberFound, nil
	}

	// Condition: unstarted member found with dataDir
	// This member is part of the cluster but has not yet started, yet has a dataDir.
	// Result: archive old dataDir and return error which will restart container
	if memberFound && targetMember.Name == "" && dataDirExists {
		archivedDir, err := archiveDataDir(o.DataDir)
		if err != nil {
			return "", memberFound, err
		}
		return "", memberFound, fmt.Errorf("member %q is unstarted but previous members dataDir exists: archiving to %q", target.String(), archivedDir)
	}

	// Condition: started member found with dataDir
	// Result: start etcd with empty initial config
	if memberFound && dataDirExists {
		return "", memberFound, nil
	}

	// Condition: started member found, no dataDir
	// A member is not actually gone forever unless it is removed from cluster with MemberRemove or the dataDir is destroyed. Since
	// this is the latter. Do not let etcd start and report the condition as an error.
	// Result: return error and restart container
	if memberFound && !dataDirExists {
		return "", memberFound, fmt.Errorf("member %q dataDir has been destroyed and must be removed from the cluster", target.String())
	}

	// Condition: member not found with dataDir
	// The member has been removed from the cluster. The dataDir will be archived once the operator
	// scales up etcd.
	// Result: retry member check allowing operator time to scale up etcd again on this node.
	if !memberFound && dataDirExists {
		return "", memberFound, fmt.Errorf("member %q not found in member list but dataDir exists, check operator logs for possible scaling problems\n", target.String())
	}

	// Condition: member not found, no dataDir
	// The member list does not reflect the target member as it is waiting to be scaled up.
	// Result: retry
	if !memberFound && !dataDirExists {
		return "", memberFound, fmt.Errorf("member %q not found in member list, check operator logs for possible scaling problems", target.String())
	}

	return "", memberFound, nil
}

func (o *DiscoverEtcdInitialClusterOptions) getClient() (*clientv3.Client, error) {
	dialOptions := []grpc.DialOption{
		grpc.WithBlock(), // block until the underlying connection is up
	}

	tlsInfo := transport.TLSInfo{
		CertFile:      o.ClientCertFile,
		KeyFile:       o.ClientKeyFile,
		TrustedCAFile: o.CABundleFile,
	}
	tlsConfig, err := tlsInfo.ClientConfig()
	if err != nil {
		return nil, err
	}

	cfg := &clientv3.Config{
		DialOptions: dialOptions,
		Endpoints:   o.Endpoints,
		DialTimeout: 2 * time.Second, // fail fast
		TLS:         tlsConfig,
	}

	return clientv3.New(*cfg)
}

func archiveDataDir(dataDir string) (string, error) {
	// for testing
	if strings.HasPrefix(dataDir, "/tmp") {
		return "/tmp-removed-archive", nil
	}
	sourceDir := filepath.Join(dataDir, "member")
	targetDir := filepath.Join(sourceDir + "-removed-archive-" + time.Now().Format("2006-01-02-030405"))

	fmt.Fprintf(os.Stderr, "attempting to archive %s to %s", sourceDir, targetDir)
	if err := os.Rename(sourceDir, targetDir); err != nil {
		return "", err
	}
	return targetDir, nil
}

func stringifyMember(member *etcdserverpb.Member) string {
	return fmt.Sprintf("{name=%q, peerURLs=[%s}, clientURLs=[%s]", member.Name, strings.Join(member.PeerURLs, ","), strings.Join(member.ClientURLs, ","))
}

// checkTargetMember populates the target member if it is part of the member list and print member details into etcd log.
func checkTargetMember(target url.URL, members []*etcdserverpb.Member) (*etcdserverpb.Member, bool) {
	for _, member := range members {
		for _, peerURL := range member.PeerURLs {
			if peerURL == target.String() {
				fmt.Fprintf(os.Stderr, "      target=%s\n", stringifyMember(member))
				return member, true
			}
		}
	}
	return nil, false
}

// logCurrentMembership prints the current etcd membership to the etcd logs.
func logCurrentMembership(members []*etcdserverpb.Member) {
	for _, member := range members {
		fmt.Fprintf(os.Stderr, "      member=%s\n", stringifyMember(member))
	}
	return
}

// formatInitialCluster populates the initial cluster comma delimited string in the format <peerName>=<peerUrl>.
func formatInitialCluster(targetName string, target *etcdserverpb.Member, members []*etcdserverpb.Member) string {
	var initialCluster []string
	for _, member := range members {
		if member.Name == "" { // this is the signal for whether or not a given peer is started
			continue
		}
		for _, peerURL := range member.PeerURLs {
			initialCluster = append(initialCluster, fmt.Sprintf("%s=%s", member.Name, peerURL))
		}
	}
	if target.Name == "" {
		// Adding unstarted member to the end of list
		initialCluster = append(initialCluster, fmt.Sprintf("%s=%s", targetName, target.PeerURLs[0]))
	}

	return strings.Join(initialCluster, ",")
}
