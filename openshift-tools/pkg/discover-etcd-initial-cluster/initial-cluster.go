package discover_etcd_initial_cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/etcd/etcdserver/etcdserverpb"

	"github.com/coreos/etcd/pkg/transport"
	"google.golang.org/grpc"

	"github.com/coreos/etcd/clientv3"

	"github.com/spf13/pflag"

	"github.com/spf13/cobra"
)

type DiscoverEtcdInitialClusterOptions struct {
	// TargetPeerURLHost is the host portion of the peer URL.  It is used to match on. (either IP or hostname)
	TargetPeerURLHost string
	// TargetName is the name to assign to this peer if we create it
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
	return &DiscoverEtcdInitialClusterOptions{}
}

func NewDiscoverEtcdInitialClusterCommand() *cobra.Command {
	o := NewDiscoverEtcdInitialCluster()

	cmd := &cobra.Command{
		Use:   "discover-etcd-initial-cluster",
		Short: "output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod",
		Long: `output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod

1. If --data-dir exists, output a marker value and exit.
2. It tries to contact every available etcd to get a list of member.
3. Check each member to see if any one of them is the target.
4. If so, and if it is started, use the member list to create the ETCD_INITIAL_CLUSTER value and print it out.
5. If so, and if it it not started, use the existing member list and append the target value to create the ETCD_INITIAL_CLUSTER value and print it out.
6. If not, try again until either you have it or you time out.
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
	return nil
}

func (o *DiscoverEtcdInitialClusterOptions) Run() error {

	//Temporary hack to work with the current pod.yaml
	var memberDir string
	if strings.HasSuffix(o.DataDir, "member") {
		memberDir = o.DataDir
		o.DataDir = filepath.Dir(o.DataDir)
	} else {
		memberDir = filepath.Join(o.DataDir, "member")
	}

	memberDirExists := false
	_, err := os.Stat(memberDir)
	switch {
	case os.IsNotExist(err):
		// do nothing. This just means we fall through to the polling logic

	case err == nil:
		fmt.Fprintf(os.Stderr, "memberDir %s is present on %s\n", memberDir, o.TargetName)
		memberDirExists = true

	case err != nil:
		return err
	}

	client, err := o.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	var targetMember *etcdserverpb.Member
	var allMembers []*etcdserverpb.Member
	for i := 0; i < 10; i++ {
		fmt.Fprintf(os.Stderr, "#### attempt %d\n", i)
		targetMember, allMembers, err = o.checkForTarget(client)

		for _, member := range allMembers {
			fmt.Fprintf(os.Stderr, "      member=%v\n", stringifyMember(member))
		}
		fmt.Fprintf(os.Stderr, "      target=%v, err=%v\n", stringifyMember(targetMember), err)

		// we're done because we found what we want.
		if targetMember != nil && err == nil {
			break
		}

		fmt.Fprintf(os.Stderr, "#### sleeping...\n")
		time.Sleep(1 * time.Second)
	}

	switch {
	case err != nil:
		return err

	case targetMember == nil && memberDirExists:
		// we weren't able to locate other members and need to return based previous memberDir so we can restart.  This is the off and on again flow.
		fmt.Printf(o.TargetName)
		return nil

	case targetMember == nil && !memberDirExists:
		// our member has not been added to the cluster and we have no previous data to start based on.
		return fmt.Errorf("timed out")

	case targetMember != nil && len(targetMember.Name) == 0 && memberDirExists:
		// our member has been added to the cluster and has never been started before, but a data directory exists. This means that we have dirty data we must remove
		archiveDataDir(memberDir)

	default:
		// a target member was found, but no exception circumstances.
	}

	etcdInitialClusterEntries := []string{}
	for _, member := range allMembers {
		if len(member.Name) == 0 { // this is the signal for whether or not a given peer is started
			continue
		}
		etcdInitialClusterEntries = append(etcdInitialClusterEntries, fmt.Sprintf("%s=%s", member.Name, member.PeerURLs[0]))
	}
	if len(targetMember.Name) == 0 {
		archiveDataDir(filepath.Clean(o.DataDir))
		etcdInitialClusterEntries = append(etcdInitialClusterEntries, fmt.Sprintf("%s=%s", o.TargetName, targetMember.PeerURLs[0]))
	}

	fmt.Printf(strings.Join(etcdInitialClusterEntries, ","))

	return nil
}

// TO DO: instead of archiving, we should remove the directory to avoid any confusion with the backups.
func archiveDataDir(sourceDir string) error {
	targetDir := filepath.Join(sourceDir+"-removed-archive", time.Now().Format(time.RFC3339))

	// If dir already exists, add seconds to the dir name
	if _, err := os.Stat(targetDir); err == nil {
		targetDir = filepath.Join(sourceDir+"-removed-archive", time.Now().Add(time.Second).Format(time.RFC3339))
	}
	if err := os.Rename(sourceDir, targetDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func stringifyMember(member *etcdserverpb.Member) string {
	if member == nil {
		return "nil"
	}

	return fmt.Sprintf("{name=%q, peerURLs=[%s}, clientURLs=[%s]", member.Name, strings.Join(member.PeerURLs, ","), strings.Join(member.ClientURLs, ","))
}

func (o *DiscoverEtcdInitialClusterOptions) checkForTarget(client *clientv3.Client) (*etcdserverpb.Member, []*etcdserverpb.Member, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	memberResponse, err := client.MemberList(ctx)
	if err != nil {
		return nil, nil, err
	}

	var targetMember *etcdserverpb.Member
	for i := range memberResponse.Members {
		member := memberResponse.Members[i]
		for _, peerURL := range member.PeerURLs {
			if strings.Contains(peerURL, o.TargetPeerURLHost) {
				targetMember = member
			}
		}
	}

	return targetMember, memberResponse.Members, err
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
		DialTimeout: 15 * time.Second,
		TLS:         tlsConfig,
	}

	return clientv3.New(*cfg)
}
