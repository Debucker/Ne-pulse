package ingest

import (
	"io"

	nepulsepb "ne-pulse/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the generated nepulsepb.TelemetryIngestServer interface.
type Server struct {
	nepulsepb.UnimplementedTelemetryIngestServer
	pool *WorkerPool
}

func NewServer(pool *WorkerPool) *Server {
	return &Server{pool: pool}
}

// StreamTelemetry reads a client-streamed sequence of TelemetryPayload
// frames. The hot path per message is exactly: decode (handled by grpc-go's
// codec before we ever see the message), copy into a pooled frame, and a
// non-blocking channel send — no relational database I/O, no locks, no
// blocking, ever, inside this loop. The single IngestResponse is only sent
// once the client half-closes the stream (io.EOF), which is inherent to
// client-streaming RPC semantics.
func (s *Server) StreamTelemetry(stream nepulsepb.TelemetryIngest_StreamTelemetryServer) error {
	var receivedCount, acceptedCount, droppedCount int64
	var firstTimestampMs, lastTimestampMs int64

	for {
		payload, err := stream.Recv()
		if err == io.EOF {
			statusLabel := "OK"
			if droppedCount > 0 {
				statusLabel = "PARTIAL_DROP"
			}
			return stream.SendAndClose(&nepulsepb.IngestResponse{
				ReceivedCount:    receivedCount,
				AcceptedCount:    acceptedCount,
				DroppedCount:     droppedCount,
				FirstTimestampMs: firstTimestampMs,
				LastTimestampMs:  lastTimestampMs,
				Status:           statusLabel,
			})
		}
		if err != nil {
			return status.Errorf(codes.Aborted, "stream read failed after %d frame(s): %v", receivedCount, err)
		}

		receivedCount++
		if receivedCount == 1 {
			firstTimestampMs = payload.GetTimestampMs()
		}
		lastTimestampMs = payload.GetTimestampMs()

		frame := AcquireFrame()
		frame.DeviceID = payload.GetDeviceId()
		frame.Latitude = payload.GetLatitude()
		frame.Longitude = payload.GetLongitude()
		frame.AccX = payload.GetAccX()
		frame.AccY = payload.GetAccY()
		frame.AccZ = payload.GetAccZ()
		frame.TimestampMs = payload.GetTimestampMs()

		if s.pool.Submit(frame) {
			acceptedCount++
		} else {
			droppedCount++
		}
	}
}
