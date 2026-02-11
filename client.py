import time
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

class CircuitBreaker:
    """简易熔断器"""
    def __init__(self, fail_max=5, reset_time=30):
        self.fail_max = fail_max
        self.reset_time = reset_time
        self.fail_count = 0
        self.last_fail_time = 0
        self.state = "CLOSED" # CLOSED, OPEN

    def can_execute(self):
        if self.state == "OPEN":
            if time.time() - self.last_fail_time > self.reset_time:
                self.state = "CLOSED"
                self.fail_count = 0
                return True
            return False
        return True

    def record_failure(self):
        self.fail_count += 1
        self.last_fail_time = time.time()
        if self.fail_count >= self.fail_max:
            self.state = "OPEN"

    def record_success(self):
        self.fail_count = 0
        self.state = "CLOSED"

class NeteaseClient:
    def __init__(self, music_u="", timeout=10):
        self.session = requests.Session()
        self.music_u = music_u
        self.timeout = timeout
        self.breaker = CircuitBreaker()
        
        # --- 核心修复：添加模拟浏览器的 Headers ---
        self.session.headers.update({
            "Accept": "*/*",
            "Accept-Language": "zh-CN,zh;q=0.9",
            "Connection": "keep-alive",
            "Host": "music.163.com",
            "Referer": "https://music.163.com",
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
        })
        
        # 配置重试机制 (对应文档 2.1)
        retry_strategy = Retry(
            total=3,
            backoff_factor=0.2, # 指数退避: 0.2s, 0.4s, 0.8s...
            status_forcelist=[429, 500, 502, 503, 504],
        )
        adapter = HTTPAdapter(max_retries=retry_strategy)
        self.session.mount("https://", adapter)
        self.session.mount("http://", adapter)

        # 设置默认 Cookie
        if self.music_u:
            self.session.cookies.set("MUSIC_U", self.music_u, domain=".163.com")

    def request(self, method, url, **kwargs):
        if not self.breaker.can_execute():
            raise Exception("熔断器已开启，请求被拦截")

        try:
            response = self.session.request(method, url, timeout=self.timeout, **kwargs)
            response.raise_for_status()
            
            # --- 增加调试逻辑：如果解析 JSON 失败，打印出原始文本 ---
            try:
                return response.json()
            except requests.exceptions.JSONDecodeError:
                print(f"DEBUG: 接口返回内容非 JSON -> {response.text[:200]}")
                raise 
                
            self.breaker.record_success()
        except Exception as e:
            self.breaker.record_failure()
            raise e