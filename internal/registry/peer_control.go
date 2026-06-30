package registry

import (
	"context"

	"google.golang.org/grpc"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

const RegistryPeerControl_LeaveCluster_FullMethodName = "/registry.v1.RegistryPeerControl/LeaveCluster"

type RegistryPeerControlServer interface {
	LeaveCluster(context.Context, *apiv1.JoinClusterRequest) (*apiv1.GossipSyncResponse, error)
}

func RegisterRegistryPeerControlServer(s grpc.ServiceRegistrar, srv RegistryPeerControlServer) {
	s.RegisterService(&RegistryPeerControl_ServiceDesc, srv)
}

func _RegistryPeerControl_LeaveCluster_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(apiv1.JoinClusterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(RegistryPeerControlServer).LeaveCluster(ctx, in)
}

var RegistryPeerControl_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "registry.v1.RegistryPeerControl",
	HandlerType: (*RegistryPeerControlServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "LeaveCluster",
			Handler:    _RegistryPeerControl_LeaveCluster_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "registry.proto",
}

