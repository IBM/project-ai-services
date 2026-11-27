# sitecustomize.py — executed automatically by every CPython interpreter startup.
#
# amdsmi ships with the ROCm RPMs as a raw source tree under
# /opt/rocm/share/amd_smi, not as a proper installed package.
# Inject the path so `import amdsmi` resolves correctly when vLLM's
# platform detection runs its ROCm check.
#
# Additionally, patch amdsmi_init()/amdsmi_shut_down() to be idempotent.
# PyTorch's torch.cuda._device_count_amdsmi() calls amdsmi_init() without
# a matching amdsmi_shut_down(), leaving the library initialised. Subsequent
# calls to amdsmi_init() on an already-initialised library return
# AMDSMI_STATUS_NOT_INIT (error 32), causing vLLM's ROCm platform detection
# to fail even though the GPU is present and accessible.
import sys
import os

_AMD_SMI_PATH = "/opt/rocm/share/amd_smi"

if os.path.isdir(_AMD_SMI_PATH) and _AMD_SMI_PATH not in sys.path:
    sys.path.insert(0, _AMD_SMI_PATH)

# Patch after path is set so the import resolves correctly.
try:
    import amdsmi as _amdsmi

    _real_init = _amdsmi.amdsmi_init
    _real_shut = _amdsmi.amdsmi_shut_down
    _init_count = [0]  # mutable int so closures can mutate it

    def _idempotent_init(*args, **kwargs):
        if _init_count[0] == 0:
            _real_init(*args, **kwargs)
        _init_count[0] += 1

    def _idempotent_shut(*args, **kwargs):
        if _init_count[0] > 0:
            _init_count[0] -= 1
        if _init_count[0] == 0:
            _real_shut(*args, **kwargs)

    _amdsmi.amdsmi_init = _idempotent_init
    _amdsmi.amdsmi_shut_down = _idempotent_shut

except Exception:
    pass  # non-ROCm environment or amdsmi unavailable — silently skip
