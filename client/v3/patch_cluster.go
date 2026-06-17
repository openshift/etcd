// Copyright 2025 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clientv3

import (
	"context"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
)

// NonLinearizeableMemberLister is used by the discover-etcd-initial-cluster command to get a list of members to ensure that *this*
// member has been added to the list.  This is needed on restart scenarios when there isn't quorum.  We need the first
// two etcd servers to start without quorum having been established by finding themselves in the member list and moving
// past the gate.
type NonLinearizeableMemberLister interface {
	// NonLinearizeableMemberList is like MemberList only without linearization.
	NonLinearizeableMemberList(ctx context.Context) (*MemberListResponse, error)
}

func (c *cluster) NonLinearizeableMemberList(ctx context.Context) (*MemberListResponse, error) {
	// it is safe to retry on list.
	resp, err := c.remote.MemberList(ctx, &pb.MemberListRequest{}, c.callOpts...)
	if err == nil {
		return (*MemberListResponse)(resp), nil
	}
	return nil, ContextError(ctx, err)
}
