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

    def get_playlist_detail(self, playlist_id: int):
        url = f"{self.base_url}/v3/playlist/detail"
        params = {"id": playlist_id}
        return self.client.request("GET", url, params=params)

    def get_lyric(self, music_id: int):
        url = f"{self.base_url}/song/lyric"
        params = {"id": music_id, "lv": -1, "kv": -1, "tv": -1}
        return self.client.request("GET", url, params=params)

    def search(self, keyword: str):
        url = f"{self.base_url}/search/get"
        params = {"s": keyword, "type": 1, "limit": 20}
        return self.client.request("GET", url, params=params)