import sys
import os
import json
import tempfile
import requests
import argparse
import subprocess
import signal

# 音频元数据处理库
import mutagen
from mutagen.id3 import ID3, TIT2, TPE1, TALB, USLT, APIC, TXXX
from mutagen.flac import FLAC, Picture
from mutagen.mp4 import MP4, MP4Cover

from client import NeteaseClient
from api import NeteaseAPI

# --- 终端颜色与日志前缀 ---
C_TITLE   = "\033[92m"  # 绿色 (标题)
C_ARTIST  = "\033[96m"  # 青色 (歌手)
C_ALBUM   = "\033[93m"  # 黄色 (专辑)
C_ID      = "\033[95m"  # 紫色 (ID)
C_RED     = "\033[91m"  # 红色 (错误)
C_END     = "\033[0m"

L_INFO    = f"\033[96m[INFO]\033[0m "
L_ERR     = f"\033[91m[ERROR]\033[0m "
L_SUCCESS = f"\033[92m[SUCCESS]\033[0m "

# --- 全局状态，用于中断清理 ---
CURRENT_FILE = None

def signal_handler(sig, frame):
    """处理 Ctrl+C 中断，清理未下载完成的残留文件"""
    global CURRENT_FILE
    print(f"\n{L_ERR}检测到用户中断 (SIGINT)")
    if CURRENT_FILE and os.path.exists(CURRENT_FILE):
        os.remove(CURRENT_FILE)
        print(f"{L_INFO}已清理未完成的文件: {os.path.basename(CURRENT_FILE)}")
    sys.exit(0)

# 注册信号捕获
signal.signal(signal.SIGINT, signal_handler)

# --- 核心初始化 ---
MUSIC_U = "COOKIES"
CLIENT = NeteaseClient(music_u=MUSIC_U)
API = NeteaseAPI(CLIENT)

def get_song_info(song):
    """
    统一处理网易云不同接口返回的字段名差异。
    搜索接口通常返回 'artists' 和 'album'，详情接口则返回 'ar' 和 'al'。
    """
    name = song.get('name', 'Unknown')
    ar_list = song.get('ar') or song.get('artists') or [{'name': 'Unknown'}]
    artist = ar_list[0].get('name', 'Unknown')
    al_dict = song.get('al') or song.get('album') or {'name': 'Unknown', 'picUrl': ''}
    album = al_dict.get('name', 'Unknown')
    pic_url = al_dict.get('picUrl', '')
    return name, artist, album, pic_url

def sanitize_path(name):
    """清洗文件名或目录名中的非法字符，防止系统报错"""
    return "".join([c for c in name if c.isalnum() or c in (' ', '.', '_', '-')]).strip()

class MetadataProcessor:
    """负责将音乐元数据（歌词、封面、信息标签）写入二进制音频文件"""
    @staticmethod
    def set_metadata(file_path, song_info, lyric, cover_url):
        ext = os.path.splitext(file_path)[1].lower()
        # 下载封面图片
        img_data = requests.get(cover_url).content if cover_url else None
        name, artist, album, _ = get_song_info(song_info)
        
        try:
            if ext == ".mp3":
                audio = ID3(file_path)
                audio.add(TIT2(encoding=3, text=name))      # 标题
                audio.add(TPE1(encoding=3, text=artist))    # 艺术家
                audio.add(TALB(encoding=3, text=album))     # 专辑
                audio.add(USLT(encoding=3, lang='eng', desc='desc', text=lyric)) # 歌词
                marker = json.dumps({"id": song_info['id'], "source": "netease"})
                audio.add(TXXX(encoding=3, desc='netease_marker', text=marker))
                if img_data:
                    audio.add(APIC(encoding=3, mime='image/jpeg', type=3, desc='Cover', data=img_data))
                audio.save()
            elif ext == ".flac":
                audio = FLAC(file_path)
                audio["title"], audio["artist"], audio["album"] = name, artist, album
                audio["lyric"] = lyric
                audio["netease_marker"] = json.dumps({"id": song_info['id']})
                if img_data:
                    pic = Picture()
                    pic.data, pic.type, pic.mime = img_data, 3, "image/jpeg"
                    audio.add_picture(pic)
                audio.save()
            elif ext == ".m4a":
                audio = MP4(file_path)
                audio["\xa9nam"], audio["\xa9ART"], audio["\xa9alb"] = name, artist, album
                audio["\xa9lyr"] = lyric
                if img_data:
                    audio["covr"] = [MP4Cover(img_data, imageformat=MP4Cover.FORMAT_JPEG)]
                audio.save()
            print(f"{L_INFO}元数据已同步完成")
        except Exception as e:
            print(f"{L_ERR}标签写入失败: {e}")

class AudioDownloader:
    """处理从网易云服务器获取音频流、降级音质匹配及本地文件落地的逻辑"""
    @staticmethod
    def process(song_id, target_quality, folder="downloads"):
        global CURRENT_FILE
        if not os.path.exists(folder):
            os.makedirs(folder)
            
        # 获取歌曲基本信息
        detail = API.get_song_detail([song_id])
        if not detail or not detail.get('songs'):
            print(f"{L_ERR}未找到歌曲ID: {song_id}")
            return None
            
        song = detail['songs'][0]
        name, artist, album, pic_url = get_song_info(song)
        
        # 音质适配：如果目标音质不可用，则按降序自动寻找可用资源
        url, actual_quality, ext = None, None, "mp3"
        qualities = ["hires", "lossless", "higher", "standard"]
        idx = qualities.index(target_quality) if target_quality in qualities else 0
        
        for q in qualities[idx:]:
            resp = API.get_song_url(song_id, q)
            if resp.get('data') and resp['data'][0].get('url'):
                url = resp['data'][0]['url']
                actual_quality = q
                ext = resp['data'][0].get('type', 'mp3').lower()
                break
        
        if not url:
            print(f"{L_ERR}歌曲 {name} 资源不可用 (可能需要会员或无版权)")
            return None

        # 构造完整文件路径
        file_name = f"{sanitize_path(name)} - {sanitize_path(artist)}.{ext}"
        file_path = os.path.join(folder, file_name)
        
        # 更新全局状态，用于 SIGINT 捕获清理
        CURRENT_FILE = file_path
        
        print(f"{L_INFO}正在下载: {C_TITLE}{name}{C_END} [音质: {actual_quality}]")
        try:
            # 下载文件
            response = requests.get(url, stream=True, timeout=30)
            response.raise_for_status()
            with open(file_path, 'wb') as f:
                for chunk in response.iter_content(chunk_size=8192):
                    f.write(chunk)
            
            # 获取歌词并写入元数据
            lrc_res = API.get_lyric(song_id)
            lrc = lrc_res.get('lrc', {}).get('lyric', '')
            MetadataProcessor.set_metadata(file_path, song, lrc, pic_url)
            
            print(f"{L_SUCCESS}保存成功: {file_path}")
            CURRENT_FILE = None # 完成后重置
            return file_path
        except Exception as e:
            print(f"{L_ERR}下载过程中出错: {e}")
            if os.path.exists(file_path): os.remove(file_path)
            return None

def print_row(col1, col2, col3, col4=None):
    """格式化控制台列表输出，使用制表符对齐"""
    row = f"{C_TITLE}{col1}{C_END}\t{C_ARTIST}{col2}{C_END}\t{C_ALBUM}{col3}{C_END}"
    if col4:
        row += f"\t{C_ID}{col4}{C_END}"
    print(row)

def cli_main():
    """解析命令行参数并分发业务逻辑"""
    global API
    parser = argparse.ArgumentParser(usage="python main.py <command> [args]")
    subparsers = parser.add_subparsers(dest="command")

    # 搜索：支持多个关键词拼接
    s_p = subparsers.add_parser("search")
    s_p.add_argument("keyword", nargs="+")

    # 下载：支持单曲、专辑、歌单模式
    dl_p = subparsers.add_parser("download")
    dl_p.add_argument("type", choices=["single", "album", "playlist"])
    dl_p.add_argument("quality", choices=["standard", "higher", "lossless", "hires"])
    dl_p.add_argument("id")

    # 播放：直接下载到临时目录后唤起播放器
    subparsers.add_parser("play").add_argument("id")
    
    # 显示：仅展示列表信息
    sh_p = subparsers.add_parser("show")
    sh_p.add_argument("type", choices=["single", "album", "playlist"])
    sh_p.add_argument("id")

    args = parser.parse_args()

    if args.command == "search":
        keyword = " ".join(args.keyword)
        res = API.search(keyword)
        for s in res.get('result', {}).get('songs', []):
            name, artist, album, _ = get_song_info(s)
            print_row(name, artist, album, s['id'])

    elif args.command == "show":
        songs = []
        if args.type == "single":
            songs = API.get_song_detail([args.id]).get('songs', [])
        elif args.type == "album":
            songs = API.get_album_detail(args.id).get('songs', [])
        elif args.type == "playlist":
            songs = API.get_playlist_detail(args.id).get('playlist', {}).get('tracks', [])
        
        for s in songs:
            name, artist, album, _ = get_song_info(s)
            print_row(name, artist, album)

    elif args.command == "download":
        ids = []
        target_dir = "downloads"
        
        # 针对不同下载类型，确定目标文件夹并搜集歌曲ID
        if args.type == "single":
            ids = [args.id]
        elif args.type == "album":
            album_res = API.get_album_detail(args.id)
            album_name = album_res.get('album', {}).get('name', 'Unknown_Album')
            target_dir = os.path.join("downloads", sanitize_path(album_name))
            ids = [s['id'] for s in album_res.get('songs', [])]
            print(f"{L_INFO}专辑模式: 将创建文件夹 {album_name}")
        elif args.type == "playlist":
            list_res = API.get_playlist_detail(args.id).get('playlist', {})
            list_name = list_res.get('name', 'Unknown_Playlist')
            target_dir = os.path.join("downloads", sanitize_path(list_name))
            ids = [s['id'] for s in list_res.get('tracks', [])]
            print(f"{L_INFO}歌单模式: 将创建文件夹 {list_name}")
        
        # 批量循环执行下载流程
        for mid in ids:
            AudioDownloader.process(mid, args.quality, target_dir)

    elif args.command == "play":
        # 临时播放模式：直接在系统临时目录下下载，不放入 downloads
        path = AudioDownloader.process(args.id, "standard", tempfile.gettempdir())
        if path:
            print(f"{L_INFO}唤起系统播放器: {os.path.basename(path)}")
            try:
                subprocess.run(["xdg-open", path], check=True)
            except Exception as e:
                print(f"{L_ERR}调用 xdg-open 失败: {e}")
    else:
        print("Usage: python main.py [search|download|show|play] [args]")

if __name__ == "__main__":
    cli_main()