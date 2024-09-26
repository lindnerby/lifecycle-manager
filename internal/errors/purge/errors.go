package purge

import "errors"

var ErrKymaNotFound = errors.New("kyma not found")
var ErrClientGetKyma = errors.New("failed getting Kyma")
var ErrClientUpdateKyma = errors.New("failed updating Kyma")
var ErrKymaNotMarkedForDeletion = errors.New("kyma not marked for deletion")
var ErrFinalizerSet = errors.New("failed setting finalizer")
var ErrPurgeNotDue = errors.New("purge not due")
