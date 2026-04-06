package gen

import "context"

// FakeService 保留一个轻量包装，便于测试或无状态场景复用通用生成服务。

type FakeService struct{}

func NewFakeService(v ...any) *FakeService { return &FakeService{} }

func (f *FakeService) SubmitTask(ctx context.Context, uid string, genTaskType string, input any) (any, error) {
	svc, err := NewService()
	if err != nil {
		return nil, err
	}
	return svc.SubmitTask(ctx, uid, genTaskType, input)
}

func (f *FakeService) QueryResult(ctx context.Context, submitID string) (any, error) {
	svc, err := NewService()
	if err != nil {
		return nil, err
	}
	return svc.QueryResult(ctx, submitID)
}
