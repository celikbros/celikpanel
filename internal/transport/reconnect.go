package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"net/rpc"
	"sync"
	"time"
)

const defaultAgentRPCCallTimeout = 25 * time.Minute

// ReconnectingClient wraps the agent RPC client and redials once when the
// underlying connection is down. net/rpc permanently shuts a client down
// after any stream error (agent restart, poisoned gob frame), which would
// otherwise sever the panel from the agent until the panel itself restarts.
//
// ReconnectingClient, agent RPC istemcisini sarar ve alttaki bağlantı
// koptuğunda bir kez yeniden bağlanır. net/rpc, herhangi bir akış hatasından
// sonra (agent yeniden başlaması, bozulmuş gob çerçevesi) istemciyi kalıcı
// olarak kapatır; bu da paneli, kendisi yeniden başlatılana dek agent'sız
// bırakırdı.
type ReconnectingClient struct {
	mu             sync.Mutex
	client         *rpc.Client
	connectContext func(context.Context) (*rpc.Client, error)
	callTimeout    time.Duration
}

func NewReconnectingClient(c *rpc.Client) *ReconnectingClient {
	return NewReconnectingClientWithContextConnector(c, ConnectAgentContext)
}

// NewReconnectingClientWithContextConnector builds a reconnecting client with
// an instance-scoped connector for dedicated, cancelable calls. Production
// uses ConnectAgentContext; tests can inject an authenticated in-memory or
// temporary-socket connector without depending on production token paths.
func NewReconnectingClientWithContextConnector(
	c *rpc.Client,
	connectContext func(context.Context) (*rpc.Client, error),
) *ReconnectingClient {
	if connectContext == nil {
		connectContext = ConnectAgentContext
	}
	return &ReconnectingClient{
		client:         c,
		connectContext: connectContext,
		callTimeout:    defaultAgentRPCCallTimeout,
	}
}

// Call mirrors rpc.Client.Call. The first failing call still returns its real
// error (honesty over silent retries); only calls that find the connection
// already dead trigger a redial and one retry.
//
// Call, rpc.Client.Call ile aynıdır. İlk başarısız çağrı gerçek hatasını
// döndürmeye devam eder (sessiz tekrar yerine dürüstlük); yalnızca bağlantıyı
// zaten ölü bulan çağrılar yeniden bağlanma ve tek bir tekrar tetikler.
func (r *ReconnectingClient) Call(serviceMethod string, args any, reply any) error {
	r.mu.Lock()
	c := r.client
	timeout := r.callTimeout
	r.mu.Unlock()

	err := callWithTimeout(c, serviceMethod, args, reply, timeout)
	if err == nil || !connDown(err) {
		return err
	}

	nc, derr := ConnectAgent()
	if derr != nil {
		// The agent is truly unreachable; the original error stands.
		// Agent gerçekten ulaşılamaz; asıl hata geçerli kalır.
		return err
	}

	r.mu.Lock()
	if r.client == c {
		_ = r.client.Close()
		r.client = nc
	} else {
		// Another caller already reconnected; use theirs.
		// Başka bir çağıran zaten yeniden bağlandı; onunkini kullan.
		_ = nc.Close()
		nc = r.client
	}
	r.mu.Unlock()

	return callWithTimeout(nc, serviceMethod, args, reply, timeout)
}

// callWithTimeout prevents legacy context-free call sites from blocking the
// panel forever. New request-bound code should still prefer CallContext so a
// disconnected HTTP client can cancel its dedicated RPC connection early.
//
// A timed-out shared stream is closed and the completed rpc.Call is drained
// before returning. That guarantees net/rpc cannot mutate the caller-owned
// reply after Call has returned; the next legacy call will reconnect normally.
func callWithTimeout(
	client *rpc.Client,
	serviceMethod string,
	args any,
	reply any,
	timeout time.Duration,
) error {
	if client == nil {
		return rpc.ErrShutdown
	}
	if timeout <= 0 {
		timeout = defaultAgentRPCCallTimeout
	}

	done := make(chan *rpc.Call, 1)
	client.Go(serviceMethod, args, reply, done)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case call := <-done:
		return call.Error
	case <-timer.C:
		_ = client.Close()
		<-done
		return context.DeadlineExceeded
	}
}

// CallContext uses a dedicated authenticated connection so cancellation can
// close the underlying stream without aborting unrelated in-flight calls on
// the shared reconnecting client. The completed rpc.Call is always drained
// before this method returns, so net/rpc cannot write to the caller-owned reply
// after return.
//
// net/rpc has no server-side per-call cancellation: once the server has
// dispatched a method, closing this client only stops transport/waiting and
// does not promise to stop the handler. Mutating agent handlers must therefore
// keep their own command timeouts and transactional safeguards.
func (r *ReconnectingClient) CallContext(
	ctx context.Context,
	serviceMethod string,
	args any,
	reply any,
) error {
	if ctx == nil {
		return errors.New("RPC call context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	connectContext := r.connectContext
	r.mu.Unlock()
	if connectContext == nil {
		connectContext = ConnectAgentContext
	}

	client, err := connectContext(ctx)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		return err
	}
	if client == nil {
		return errors.New("agent context connector returned a nil RPC client")
	}
	if err := ctx.Err(); err != nil {
		_ = client.Close()
		return err
	}

	done := make(chan *rpc.Call, 1)
	client.Go(serviceMethod, args, reply, done)
	select {
	case <-ctx.Done():
		_ = client.Close()
		<-done
		return ctx.Err()
	case call := <-done:
		_ = client.Close()
		return call.Error
	}
}

// connDown reports whether the error means the RPC connection itself is dead
// (as opposed to a server-side error for this one call).
// connDown, hatanın (bu tek çağrıya özgü sunucu hatası değil) RPC
// bağlantısının kendisinin öldüğü anlamına gelip gelmediğini bildirir.
func connDown(err error) bool {
	if errors.Is(err, rpc.ErrShutdown) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
