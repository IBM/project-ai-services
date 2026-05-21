"""
Diagnostic Logger Framework

This module provides simplified system diagnostics logging for crash scenarios.
It captures critical system metrics before the application crashes, including:
- Process information (PID count, limits, peak usage)
- Memory usage (current usage and limit)
- CPU usage (current usage and limit)
- File descriptor usage and limits
- Disk usage statistics
"""

import os
import sys
import logging
import psutil
import shutil
import traceback
from typing import Dict, Any, Optional
from pathlib import Path
import time
import threading


def _run_with_timeout(func, args=(), kwargs=None, timeout=1.0, default=None):
    """
    Run a function with a timeout to prevent hanging on network mounts.
    
    Args:
        func: Function to execute
        args: Positional arguments for the function
        kwargs: Keyword arguments for the function
        timeout: Timeout in seconds (default: 1.0s)
        default: Default value to return on timeout
    
    Returns:
        Function result or default value on timeout
    """
    if kwargs is None:
        kwargs = {}
    
    result = [default]
    exception: list[Optional[Exception]] = [None]
    
    def target():
        try:
            result[0] = func(*args, **kwargs)
        except Exception as e:
            exception[0] = e
    
    thread = threading.Thread(target=target, daemon=True)
    thread.start()
    thread.join(timeout)
    
    if thread.is_alive():
        # Thread is still running - timeout occurred
        return default
    
    if exception[0]:
        raise exception[0]
    
    return result[0]


class DiagnosticLogger:
    """
    Captures and logs comprehensive system diagnostics.
    
    This class provides methods to collect system metrics and log them
    in a structured format, particularly useful for debugging crashes.
    """
    
    def __init__(self, logger: Optional[logging.Logger] = None):
        """
        Initialize the diagnostic logger.
        
        Args:
            logger: Optional logger instance. If not provided, uses root logger.
        """
        self.logger = logger or logging.getLogger(__name__)
        self.process = psutil.Process(os.getpid())
        self.cooldown_seconds = 3
        self._last_diagnostic_dump_time = 0.0
        self.disk_check_timeout = 1.0  # 1000ms timeout for disk operations
    
    def get_process_info(self) -> Dict[str, Any]:
        """
        Collect process-related information.
        
        Returns:
            Dictionary containing PID count, limits, and peak usage.
        """
        try:
            # Get current process info
            current_pid = os.getpid()
            parent_pid = os.getppid()
            
            # Get all child processes
            children = self.process.children(recursive=True)
            child_pids = [p.pid for p in children]
            
            # Get cgroup PID limits (cgroup v2)
            pid_limits = self._get_cgroup_pid_limits()
            
            # Get number of threads
            num_threads = self.process.num_threads()
            
            return {
                "current_pid": current_pid,
                "parent_pid": parent_pid,
                "child_processes": len(children),
                "child_pids": child_pids,
                "total_process_count": 1 + len(children),
                "thread_count": num_threads,
                "pid_limits": pid_limits,
                "process_status": self.process.status(),
                "process_create_time": self.process.create_time()
            }
        except Exception as e:
            self.logger.error(f"Error collecting process info: {e}")
            return {"error": str(e)}
    
    def _get_cgroup_pid_limits(self) -> Dict[str, Any]:
        """
        Get PID limits from cgroup v2 filesystem.
        
        Returns:
            Dictionary with current, max, and peak PID counts from cgroup.
        """
        pid_info = {}
        
        try:
            # Try to read cgroup v2 pids.current (equivalent to cat /sys/fs/cgroup/pids.current)
            cgroup_paths = [
                "/sys/fs/cgroup/pids.current",  # cgroup v2 unified hierarchy
                f"/sys/fs/cgroup/pids/pids.current",  # cgroup v1
            ]
            
            for path in cgroup_paths:
                try:
                    if os.path.exists(path):
                        with open(path, 'r') as f:
                            pid_info["current"] = int(f.read().strip())
                        break
                except (IOError, ValueError) as e:
                    self.logger.debug(f"Could not read {path}: {e}")
            
            # Try to read pids.max (limit)
            max_paths = [
                "/sys/fs/cgroup/pids.max",  # cgroup v2
                f"/sys/fs/cgroup/pids/pids.max",  # cgroup v1
            ]
            
            for path in max_paths:
                try:
                    if os.path.exists(path):
                        with open(path, 'r') as f:
                            max_val = f.read().strip()
                            pid_info["max"] = max_val if max_val != "max" else "unlimited"
                        break
                except (IOError, ValueError) as e:
                    self.logger.debug(f"Could not read {path}: {e}")
            
            # Try to read pids.peak (cgroup v2 only)
            peak_path = "/sys/fs/cgroup/pids.peak"
            try:
                if os.path.exists(peak_path):
                    with open(peak_path, 'r') as f:
                        pid_info["peak"] = int(f.read().strip())
            except (IOError, ValueError) as e:
                self.logger.debug(f"Could not read {peak_path}: {e}")
            
            # Fallback to resource limits if cgroup info not available
            if not pid_info:
                try:
                    import resource
                    soft_limit, hard_limit = resource.getrlimit(resource.RLIMIT_NPROC)
                    pid_info = {
                        "soft_limit": soft_limit if soft_limit != resource.RLIM_INFINITY else "unlimited",
                        "hard_limit": hard_limit if hard_limit != resource.RLIM_INFINITY else "unlimited",
                        "note": "Using RLIMIT_NPROC (user process limit, not cgroup)"
                    }
                except (ImportError, AttributeError):
                    pid_info = {"error": "No cgroup or resource limit info available"}
        
        except Exception as e:
            self.logger.error(f"Error getting cgroup PID limits: {e}")
            pid_info = {"error": str(e)}
        
        return pid_info
    
    def get_memory_info(self) -> Dict[str, Any]:
        """
        Collect memory usage information.
        
        Returns:
            Dictionary containing current memory usage and limit.
        """
        try:
            # Process memory info
            mem_info = self.process.memory_info()
            
            # System memory info
            virtual_mem = psutil.virtual_memory()
            
            # Memory limit (if available)
            limit = "unlimited"
            try:
                import resource
                soft_limit, hard_limit = resource.getrlimit(resource.RLIMIT_AS)
                if soft_limit != resource.RLIM_INFINITY:
                    limit = f"{round(soft_limit / (1024 ** 3), 2)} GB"
            except (ImportError, AttributeError):
                pass
            
            return {
                "current_usage": f"{round(mem_info.rss / (1024 ** 3), 2)} GB",
                "limit": limit,
                "system_total": f"{round(virtual_mem.total / (1024 ** 3), 2)} GB"
            }
        except Exception as e:
            self.logger.error(f"Error collecting memory info: {e}")
            return {"error": str(e)}
    
    def get_cpu_info(self) -> Dict[str, Any]:
        """
        Collect CPU usage information.
        
        Returns:
            Dictionary containing current CPU usage and limit.
        """
        try:
            # Process CPU info
            cpu_percent = self.process.cpu_percent(interval=0.1)
            
            # CPU limit (if available)
            limit = "unlimited"
            try:
                import resource
                soft_limit, hard_limit = resource.getrlimit(resource.RLIMIT_CPU)
                if soft_limit != resource.RLIM_INFINITY:
                    limit = f"{soft_limit} seconds"
            except (ImportError, AttributeError):
                pass
            
            # Get CPU count for context
            cpu_count = psutil.cpu_count(logical=True)
            
            return {
                "current_usage": f"{round(cpu_percent, 2)}%",
                "limit": limit,
                "available_cores": cpu_count
            }
        except Exception as e:
            self.logger.error(f"Error collecting CPU info: {e}")
            return {"error": str(e)}
    
    def get_numa_info(self) -> Dict[str, Any]:
        """
        Collect NUMA node information.
        
        Returns:
            Dictionary containing NUMA node count.
        """
        try:
            import subprocess
            
            # Try to get NUMA node count using lscpu
            try:
                result = subprocess.run(
                    ['lscpu'],
                    capture_output=True,
                    text=True,
                    timeout=2
                )
                
                if result.returncode == 0:
                    # Parse lscpu output for NUMA node(s) line
                    for line in result.stdout.splitlines():
                        if 'NUMA node(s):' in line:
                            fields = line.split()
                            if fields:
                                numa_count = int(fields[-1])
                                return {"numa_node_count": numa_count}
            except (subprocess.TimeoutExpired, subprocess.SubprocessError, ValueError) as e:
                self.logger.debug(f"Could not get NUMA info via lscpu: {e}")
            
            # Fallback: try to read from /sys/devices/system/node/
            try:
                node_path = Path("/sys/devices/system/node")
                if node_path.exists():
                    # Count node* directories
                    numa_nodes = [d for d in node_path.iterdir() if d.is_dir() and d.name.startswith('node')]
                    numa_count = len(numa_nodes)
                    if numa_count > 0:
                        return {"numa_node_count": numa_count}
            except Exception as e:
                self.logger.debug(f"Could not read NUMA info from /sys: {e}")
            
            return {"numa_node_count": "N/A"}
        
        except Exception as e:
            self.logger.error(f"Error collecting NUMA info: {e}")
            return {"error": str(e)}
    
    def get_file_descriptor_info(self) -> Dict[str, Any]:
        """
        Collect file descriptor usage information.
        
        Returns:
            Dictionary containing FD count, limits, and open files.
        """
        try:
            # Get open files
            try:
                open_files = self.process.open_files()
                num_open_files = len(open_files)
                open_file_paths = [f.path for f in open_files[:10]]  # Limit to first 10
            except (psutil.AccessDenied, AttributeError):
                num_open_files = "Access Denied"
                open_file_paths = []
            
            # Get file descriptor limits
            try:
                import resource
                soft_limit, hard_limit = resource.getrlimit(resource.RLIMIT_NOFILE)
                fd_limits = {
                    "soft_limit": soft_limit if soft_limit != resource.RLIM_INFINITY else "unlimited",
                    "hard_limit": hard_limit if hard_limit != resource.RLIM_INFINITY else "unlimited"
                }
            except (ImportError, AttributeError):
                fd_limits = {"soft_limit": "N/A", "hard_limit": "N/A"}
            
            # Get number of file descriptors (Unix-like systems)
            try:
                num_fds = self.process.num_fds()
            except (AttributeError, psutil.AccessDenied):
                num_fds = "N/A"
            
            return {
                "num_file_descriptors": num_fds,
                "num_open_files": num_open_files,
                "open_files_sample": open_file_paths,
                "limits": fd_limits
            }
        except Exception as e:
            self.logger.error(f"Error collecting file descriptor info: {e}")
            return {"error": str(e)}
    
    def get_disk_usage(self, paths: Optional[list] = None) -> Dict[str, Any]:
        """
        Collect disk usage information with timeout protection.
        
        Uses a short timeout (1000ms) to prevent hanging on unresponsive
        network mounts (NFS, SMB, etc.) especially on macOS.
        
        Args:
            paths: List of paths to check. Defaults to ["/", "/tmp", current working directory].
        
        Returns:
            Dictionary containing disk usage for specified paths (in GB only).
        """
        try:
            if paths is None:
                paths = ["/", "/tmp", "/var", os.getcwd()]
            
            disk_info = {}
            for path in paths:
                try:
                    # Check if path exists with timeout
                    path_exists = _run_with_timeout(
                        os.path.exists,
                        args=(path,),
                        timeout=self.disk_check_timeout,
                        default=False
                    )
                    
                    if path_exists:
                        # Get disk usage with timeout to prevent hanging on network mounts
                        usage = _run_with_timeout(
                            shutil.disk_usage,
                            args=(path,),
                            timeout=self.disk_check_timeout,
                            default=None
                        )
                        
                        if usage is not None:
                            disk_info[path] = {
                                "total_gb": round(usage.total / (1024 ** 3), 2),
                                "used_gb": round(usage.used / (1024 ** 3), 2),
                                "free_gb": round(usage.free / (1024 ** 3), 2),
                                "percent": round((usage.used / usage.total) * 100, 2)
                            }
                        else:
                            disk_info[path] = {"error": "timeout - possible network mount hang"}
                    else:
                        disk_info[path] = {"error": "path does not exist or timed out"}
                except Exception as e:
                    disk_info[path] = {"error": str(e)}
            
            return disk_info
        except Exception as e:
            self.logger.error(f"Error collecting disk usage: {e}")
            return {"error": str(e)}
    
    def get_network_connections(self) -> Dict[str, Any]:
        """
        Collect network connection information.
        
        Returns:
            Dictionary containing network connection statistics.
        """
        try:
            connections = self.process.net_connections(kind='inet')
            
            connection_summary = {
                "total_connections": len(connections),
                "by_status": {},
                "by_type": {}
            }
            
            for conn in connections:
                # Count by status
                status = conn.status
                connection_summary["by_status"][status] = connection_summary["by_status"].get(status, 0) + 1
                
                # Count by type
                conn_type = str(conn.type)
                connection_summary["by_type"][conn_type] = connection_summary["by_type"].get(conn_type, 0) + 1
            
            return connection_summary
        except (psutil.AccessDenied, AttributeError) as e:
            return {"error": f"Access denied or not available: {e}"}
        except Exception as e:
            self.logger.error(f"Error collecting network info: {e}")
            return {"error": str(e)}
    
    def log_all_diagnostics(self, exception: Optional[Exception] = None, 
                           extra_context: Optional[Dict[str, Any]] = None):
        """
        Log all diagnostic information in a structured format.
        
        Args:
            exception: Optional exception that triggered the diagnostic logging.
            extra_context: Optional dictionary with additional context to log.
        """
        current_time = time.monotonic()
        if current_time - self._last_diagnostic_dump_time < self.cooldown_seconds:
            return
        self._last_diagnostic_dump_time = current_time

        self.logger.info("=" * 80)
        self.logger.info("DIAGNOSTIC INFORMATION - SYSTEM CRASH DETECTED")
        self.logger.info("=" * 80)
        
        # Log exception information if provided
        if exception:
            self.logger.info(f"Exception Type: {type(exception).__name__}")
            self.logger.info(f"Exception Message: {str(exception)}")
            self.logger.info("Traceback:")
            self.logger.info(traceback.format_exc())
        
        # Log extra context if provided
        if extra_context:
            self.logger.info(f"Extra Context: {extra_context}")
        
        # Log all metrics
        self.logger.info("--- PROCESS INFORMATION ---")
        process_info = self.get_process_info()
        for key, value in process_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("--- MEMORY INFORMATION ---")
        memory_info = self.get_memory_info()
        for key, value in memory_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("--- CPU INFORMATION ---")
        cpu_info = self.get_cpu_info()
        for key, value in cpu_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("--- NUMA INFORMATION ---")
        numa_info = self.get_numa_info()
        for key, value in numa_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("--- FILE DESCRIPTOR INFORMATION ---")
        fd_info = self.get_file_descriptor_info()
        for key, value in fd_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("--- DISK USAGE ---")
        disk_info = self.get_disk_usage()
        for path, data in disk_info.items():
            self.logger.info(f"Path: {path}")
            if isinstance(data, dict):
                for key, value in data.items():
                    self.logger.info(f"  {key}: {value}")
        
        self.logger.info("--- NETWORK CONNECTIONS ---")
        network_info = self.get_network_connections()
        for key, value in network_info.items():
            self.logger.info(f"{key}: {value}")
        
        self.logger.info("=" * 80)
        self.logger.info("END OF DIAGNOSTIC INFORMATION")
        self.logger.info("=" * 80)


def setup_crash_handler(logger: Optional[logging.Logger] = None):
    """
    Set up a global exception handler that logs diagnostics before crash.
    
    Args:
        logger: Optional logger instance to use for diagnostics.
    
    Returns:
        The DiagnosticLogger instance for manual use if needed.
    """
    diagnostic_logger = DiagnosticLogger(logger)
    
    def exception_handler(exc_type, exc_value, exc_traceback):
        """Global exception handler that logs diagnostics."""
        # Don't catch KeyboardInterrupt
        if issubclass(exc_type, KeyboardInterrupt):
            sys.__excepthook__(exc_type, exc_value, exc_traceback)
            return
        
        # Log diagnostics
        diagnostic_logger.log_all_diagnostics(
            exception=exc_value,
            extra_context={
                "exception_type": exc_type.__name__,
                "python_version": sys.version,
                "platform": sys.platform
            }
        )
        
        # Call the default exception handler
        sys.__excepthook__(exc_type, exc_value, exc_traceback)
    
    # Set the global exception handler
    sys.excepthook = exception_handler
    
    return diagnostic_logger

import signal
import select


class StderrMonitor:
    """
    Monitor stderr for C-level library errors (e.g., libgomp, BLAS, MKL).
    
    This catches errors that don't raise Python exceptions but print to stderr.
    """
    
    def __init__(self, logger: logging.Logger, diagnostic_logger: DiagnosticLogger):
        """
        Initialize stderr monitor.
        
        Args:
            logger: Logger instance for warnings
            diagnostic_logger: DiagnosticLogger instance for detailed dumps
        """
        self.logger = logger
        self.diagnostic_logger = diagnostic_logger
        self.original_stderr = sys.stderr
        self._stop_event = threading.Event()
        self._monitor_thread: Optional[threading.Thread] = None
        
        # Error patterns to watch for
        self.error_patterns = [
            b'libgomp: Thread creation failed',
            b'BLAS: Program is Terminated',
            b'MKL ERROR',
            b'OpenBLAS',
            b'CUDA error',
            b'TensorFlow',
            b'PyTorch',
            b'Resource temporarily unavailable',
            b'Cannot allocate memory',
            b'Out of memory',
        ]
    
    def start(self):
        """Start monitoring stderr"""
        if self._monitor_thread is not None and self._monitor_thread.is_alive():
            self.logger.warning("stderr monitor already running")
            return
        
        try:
            # Create a pipe to capture stderr
            self.read_fd, self.write_fd = os.pipe()
            
            # Duplicate stderr to our pipe
            self.original_stderr_fd = os.dup(sys.stderr.fileno())
            os.dup2(self.write_fd, sys.stderr.fileno())
            
            # Start monitoring thread
            self._stop_event.clear()
            self._monitor_thread = threading.Thread(
                target=self._monitor_loop,
                name="StderrMonitor",
                daemon=True
            )
            self._monitor_thread.start()
            self.logger.info("stderr monitor started")
        except Exception as e:
            self.logger.error(f"Failed to start stderr monitor: {e}")
    
    def stop(self):
        """Stop monitoring stderr"""
        if self._monitor_thread is None:
            return
        
        try:
            self._stop_event.set()
            
            # Restore original stderr
            if hasattr(self, 'original_stderr_fd'):
                os.dup2(self.original_stderr_fd, sys.stderr.fileno())
                os.close(self.original_stderr_fd)
            
            # Close pipe
            if hasattr(self, 'write_fd'):
                os.close(self.write_fd)
            if hasattr(self, 'read_fd'):
                os.close(self.read_fd)
            
            self._monitor_thread.join(timeout=2)
            self.logger.info("stderr monitor stopped")
        except Exception as e:
            self.logger.error(f"Error stopping stderr monitor: {e}")
    
    def _monitor_loop(self):
        """Monitor stderr for error patterns"""
        try:
            with os.fdopen(self.read_fd, 'rb', buffering=0) as f:
                while not self._stop_event.is_set():
                    # Use select with timeout to allow checking stop event
                    ready, _, _ = select.select([f], [], [], 0.1)
                    if not ready:
                        continue
                    
                    line = f.readline()
                    if not line:
                        break
                    
                    # Write to original stderr
                    try:
                        os.write(self.original_stderr_fd, line)
                    except:
                        pass
                    
                    # Check for error patterns
                    for pattern in self.error_patterns:
                        if pattern in line:
                            error_msg = line.decode('utf-8', errors='ignore').strip()
                            
                            # Log full diagnostics
                            self.diagnostic_logger.log_all_diagnostics(
                                extra_context={
                                    "trigger": "stderr_monitor",
                                    "error_line": error_msg,
                                    "error_type": "c_level_library_error",
                                    "pattern_matched": pattern.decode('utf-8', errors='ignore')
                                }
                            )
                            break
        except Exception as e:
            self.logger.error(f"Error in stderr monitor loop: {e}")


class SignalHandler:
    """
    Handle fatal signals (SIGSEGV, SIGABRT, etc.) and log diagnostics.
    
    This catches crashes that don't go through Python's exception handling.
    """
    
    def __init__(self, logger: logging.Logger, diagnostic_logger: DiagnosticLogger):
        """
        Initialize signal handler.
        
        Args:
            logger: Logger instance
            diagnostic_logger: DiagnosticLogger instance for detailed dumps
        """
        self.logger = logger
        self.diagnostic_logger = diagnostic_logger
        self.original_handlers = {}
    
    def setup(self):
        """Setup signal handlers for fatal signals"""
        # Signals to handle
        signals_to_handle = []
        
        # SIGSEGV - Segmentation fault
        if hasattr(signal, 'SIGSEGV'):
            signals_to_handle.append(signal.SIGSEGV)
        
        # SIGABRT - Abort signal
        if hasattr(signal, 'SIGABRT'):
            signals_to_handle.append(signal.SIGABRT)
        
        # SIGBUS - Bus error
        if hasattr(signal, 'SIGBUS'):
            signals_to_handle.append(signal.SIGBUS)
        
        # SIGFPE - Floating point exception
        if hasattr(signal, 'SIGFPE'):
            signals_to_handle.append(signal.SIGFPE)
        
        # SIGILL - Illegal instruction
        if hasattr(signal, 'SIGILL'):
            signals_to_handle.append(signal.SIGILL)
        
        for sig in signals_to_handle:
            try:
                # Save original handler
                self.original_handlers[sig] = signal.signal(sig, self._signal_handler)
            except (OSError, ValueError) as e:
                # Some signals can't be caught
                self.logger.debug(f"Cannot handle signal {sig}: {e}")
        
        if signals_to_handle:
            self.logger.info(f"Signal handlers installed for: {[signal.Signals(s).name for s in signals_to_handle]}")
    
    def _signal_handler(self, signum, frame):
        """Handle fatal signals"""
        signal_name = signal.Signals(signum).name if hasattr(signal, 'Signals') else str(signum)
        
        self.logger.info(f"⚠️  Received fatal signal: {signal_name} ({signum})")
        
        # Log diagnostics
        self.diagnostic_logger.log_all_diagnostics(
            extra_context={
                "trigger": "signal_handler",
                "signal": signum,
                "signal_name": signal_name,
                "frame_info": str(frame) if frame else None
            }
        )
        
        # Restore original handler and re-raise
        if signum in self.original_handlers:
            signal.signal(signum, self.original_handlers[signum])
        else:
            signal.signal(signum, signal.SIG_DFL)
        
        # Re-raise the signal
        os.kill(os.getpid(), signum)


def setup_comprehensive_crash_handler(logger: logging.Logger) -> tuple:
    """
    Setup comprehensive crash handling with three layers:
    1. Python exception handler (sys.excepthook)
    2. stderr monitor (for C-level library errors)
    3. Signal handler (for segfaults and fatal signals)
    
    Args:
        logger: Logger instance to use for diagnostics
    
    Returns:
        Tuple of (diagnostic_logger, stderr_monitor, signal_handler)
    
    Example:
        logger = get_logger("app")
        diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)
        
        # In lifespan shutdown:
        stderr_monitor.stop()
    """
    # Layer 1: Python exception handler
    diagnostic_logger = setup_crash_handler(logger)
    
    # Layer 2: stderr monitor for C-level errors
    stderr_monitor = StderrMonitor(logger, diagnostic_logger)
    stderr_monitor.start()
    
    # Layer 3: Signal handler for fatal signals
    signal_handler = SignalHandler(logger, diagnostic_logger)
    signal_handler.setup()
    
    logger.info("Comprehensive crash handler initialized (Python exceptions + C errors + Signals)")
    
    return diagnostic_logger, stderr_monitor, signal_handler

