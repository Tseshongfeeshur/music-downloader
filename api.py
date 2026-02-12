from client import NeteaseClient

class NeteaseAPI:
    def __init__(self, client):
        self.client = client
        self.base_url = "https://music.163.com/api"

    def get_song_detail(self, music_ids: list):
        url = f"{self.base_url}/song/detail"
        params = {"ids": f"[{','.join(map(str, music_ids))}]"}
        return self.client.request("GET", url, params=params)

    def get_song_url(self, music_id: int, quality="standard"):
        url = "https://music.163.com/api/song/enhance/player/url"
        quality_map = {"standard": 128000, "higher": 320000, "lossless": 999000, "hires": 1999000}
        params = {"ids": f"[{music_id}]", "br": quality_map.get(quality, 128000)}
        return self.client.request("GET", url, params=params)

    def get_album_detail(self, album_id: int):
        url = f"{self.base_url}/album/{album_id}"
        return self.client.request("GET", url)

    def get_playlist_detail(self, playlist_id: int, n=1000):
        """
        获取歌单详情。
        由于 v3 接口的 tracks 字段默认只返回前10首，
        我们需要根据 trackIds 重新请求完整的歌曲详情。
        """
        url = f"{self.base_url}/v3/playlist/detail"
        params = {"id": playlist_id, "n": n} # n 为获取 trackIds 的数量
        res = self.client.request("GET", url, params=params)
        
        playlist = res.get('playlist', {})
        track_ids = [t.get('id') for t in playlist.get('trackIds', [])]
        
        # 如果 trackIds 数量超过了当前 tracks 的数量（通常是10），则重新拉取完整详情
        if track_ids and len(track_ids) > len(playlist.get('tracks', [])):
            # 网易云接口通常限制单次详情请求为 1000 首
            full_details = self.get_song_detail(track_ids[:n])
            if full_details and full_details.get('songs'):
                playlist['tracks'] = full_details['songs']
        
        return res

    def get_lyric(self, music_id: int):
        url = f"{self.base_url}/song/lyric"
        params = {"id": music_id, "lv": -1, "kv": -1, "tv": -1}
        return self.client.request("GET", url, params=params)

    def search(self, keyword: str):
        url = f"{self.base_url}/search/get"
        params = {"s": keyword, "type": 1, "limit": 20}
        return self.client.request("GET", url, params=params)
