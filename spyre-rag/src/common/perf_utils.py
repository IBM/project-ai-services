import threading
import time
from datetime import datetime
from collections import deque

class PerfMetricsRegistry:
    def __init__(self, max_size=1000):
        self._metrics = deque(maxlen=max_size)
        self._lock = threading.Lock()

    def add_metric(self, metric):
        # Store as float for precision but we can convert for output
        metric["timestamp"] = time.time()
        # Also add a readable string version
        metric["readable_timestamp"] = datetime.fromtimestamp(metric["timestamp"]).strftime('%Y-%m-%d %H:%M:%S')
        with self._lock:
            self._metrics.append(metric)

    def get_metrics(self):
        with self._lock:
            return list(self._metrics)

# Global registry instance
perf_registry = PerfMetricsRegistry()
