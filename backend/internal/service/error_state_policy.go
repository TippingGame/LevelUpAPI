package service

func shouldApplyLocalErrorState(account *Account, statusCode int) bool {
	if account == nil {
		return false
	}
	if account.IsPoolMode() && !account.IsCustomErrorCodesEnabled() {
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
	if account.IsPoolMode() && !account.IsCustomErrorCodesEnabled() {
		return false
	}
	return true
}
