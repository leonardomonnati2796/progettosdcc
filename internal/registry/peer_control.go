package registry

import (
	"context"

	"google.golang.org/grpc"

	apiv1 "github.com/leonardomonnati2796/distributed-service-registry/pkg/api"
)

const RegPeerControl_LeaveCluster_FullMethodName = "/registry.v1.RegPeerControl/LeaveCluster"

type RegPeerControlServer interface {
	LeaveCluster(context.Context, *apiv1.JoinNodeRequest) (*apiv1.GossipUpdResponse, error)
}

func RegisterRegPeerControlServer(s grpc.ServiceRegistrar, srv RegPeerControlServer) {
	// Registra registry peer control server.
	s.RegisterService(&RegPeerControl_ServiceDesc, srv)
}

func _RegPeerControl_LeaveCluster_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
	// Esegue la logica di registry peer control leave cluster handler.
	in := new(apiv1.JoinNodeRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(RegPeerControlServer).LeaveCluster(ctx, in)
}

var RegPeerControl_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "registry.v1.RegPeerControl",
	HandlerType: (*RegPeerControlServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "LeaveCluster",
			Handler:    _RegPeerControl_LeaveCluster_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "registry.proto",
}
