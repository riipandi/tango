package queue

import "time"

type testTask struct {
	Val string
}

func (t testTask) Config() QueueConfig {
	return QueueConfig{
		Name:        "test",
		MaxAttempts: 2,
		Backoff:     5 * time.Millisecond,
		Timeout:     time.Second,
		Retention: &Retention{
			Duration:   time.Hour,
			OnlyFailed: false,
			Data: &RetainData{
				OnlyFailed: false,
			},
		},
	}
}

type testTaskRetainForever struct {
	Val string
}

func (t testTaskRetainForever) Config() QueueConfig {
	return QueueConfig{
		Name:        "test-retainforever",
		MaxAttempts: 2,
		Backoff:     8 * time.Millisecond,
		Timeout:     2 * time.Second,
		Retention: &Retention{
			OnlyFailed: false,
		},
	}
}

type testTaskNoRetention struct {
	Val string
}

func (t testTaskNoRetention) Config() QueueConfig {
	return QueueConfig{
		Name:        "test-noret",
		MaxAttempts: 2,
		Backoff:     8 * time.Millisecond,
		Timeout:     2 * time.Second,
		Retention:   nil,
	}
}

type testTaskRetainFailed struct {
	Val string
}

func (t testTaskRetainFailed) Config() QueueConfig {
	return QueueConfig{
		Name:        "test-retainfailed",
		MaxAttempts: 2,
		Backoff:     8 * time.Millisecond,
		Timeout:     2 * time.Second,
		Retention: &Retention{
			OnlyFailed: true,
		},
	}
}

type testTaskRetainNoData struct {
	Val string
}

func (t testTaskRetainNoData) Config() QueueConfig {
	return QueueConfig{
		Name:        "test-retainnodata",
		MaxAttempts: 2,
		Backoff:     5 * time.Millisecond,
		Timeout:     time.Second,
		Retention: &Retention{
			Duration:   time.Hour,
			OnlyFailed: false,
			Data:       nil,
		},
	}
}

type testTaskRetainDataFailed struct {
	Val string
}

func (t testTaskRetainDataFailed) Config() QueueConfig {
	return QueueConfig{
		Name:        "test-retaindatafailed",
		MaxAttempts: 2,
		Backoff:     5 * time.Millisecond,
		Timeout:     time.Second,
		Retention: &Retention{
			Duration:   time.Hour,
			OnlyFailed: false,
			Data: &RetainData{
				OnlyFailed: true,
			},
		},
	}
}

type testTaskNoName struct {
	Val string
}

func (t testTaskNoName) Config() QueueConfig {
	return QueueConfig{
		Name: "",
	}
}

type testTaskEncodeFail struct {
	Val chan int
}

func (t testTaskEncodeFail) Config() QueueConfig {
	return QueueConfig{
		Name: "encode-failer",
	}
}
