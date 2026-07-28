package main

// packageManagerMutationBusyProbe keeps the host probe injectable for isolated
// unit tests. Production initializes it to the platform-specific implementation.
// packageManagerMutationBusyProbe, host probunu yalıtılmış birim testlerinde
// değiştirilebilir tutar. Üretim onu platforma özgü uygulamayla başlatır.
var packageManagerMutationBusyProbe = realPackageManagerMutationBusy

func packageManagerMutationBusy() (bool, error) {
	return packageManagerMutationBusyProbe()
}
