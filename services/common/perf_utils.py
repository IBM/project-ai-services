import threading
import time
from datetime import datetime
from collections import OrderedDict
from common.misc_utils import get_request_id

class PerfMetricsRegistry:
    def __init__(self, max_size=1000):
        self._metrics = OrderedDict()
        self._max_size = max_size
        self._lock = threading.Lock()

    def add_metric(self, metric):
        # Store as float for precision but we can convert for output
        metric["timestamp"] = time.time()
        # Also add a readable string version
        metric["readable_timestamp"] = datetime.fromtimestamp(metric["timestamp"]).strftime('%Y-%m-%d %H:%M:%S')
        # Capture request_id from context
        request_id = get_request_id()
        metric["request_id"] = request_id
        
        with self._lock:
            # Store metric with request_id as key
            self._metrics[request_id] = metric
            
            # Remove oldest entry if we exceed max_size
            if len(self._metrics) > self._max_size:
                self._metrics.popitem(last=False)  # Remove oldest (FIFO)

    def get_metrics(self):
        """Return all metrics as a list"""
        with self._lock:
            return list(self._metrics.values())
    
    def get_metric_by_request_id(self, request_id):
        """Return a specific metric by request_id"""
        with self._lock:
            return self._metrics.get(request_id)

# Global registry instance
perf_registry = PerfMetricsRegistry()
