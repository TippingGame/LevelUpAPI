package service

func shouldApplyLocalErrorState(account *Account, statusCode int) bool {
	if account == nil {
		return false
	}
	if account.IsPoolMode() && !account.HasActiveCustomErrorCodePolicy() {
		return false
	}
	if account.IsCustomErrorCodesEnabled() && !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	return true
}

func shouldApplyLocalSystemErrorState(account *Account) bool {
	if account == nil {
		return false
	}
	if account.IsPoolMode() && !account.HasActiveCustomErrorCodePolicy() {
		return false
	}
	return true
}
