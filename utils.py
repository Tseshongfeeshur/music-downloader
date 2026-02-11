import re

class URLMatcher:
    """URL 匹配与解析 (对应文档 第四章)"""
    
    @staticmethod
    def match_song_id(url: str):
        """支持多种格式的歌曲 ID 提取"""
        patterns = [
            r"song\?id=(\d+)",
            r"song/(\d+)",
        ]
        for p in patterns:
            match = re.search(p, url)
            if match:
                return match.group(1)
        return None

    @staticmethod
    def match_playlist_id(url: str):
        """支持歌单和专辑解析"""
        if "playlist" in url:
            match = re.search(r"id=(\d+)", url)
            return match.group(1) if match else None
        elif "album" in url:
            match = re.search(r"id=(\d+)", url)
            # 模拟文档中的 album:xxxxx 格式
            return f"album:{match.group(1)}" if match else None
        return None