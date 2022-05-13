from argparse import ArgumentParser

from monday import MondayClient

apiKey = """eyJhbGciOiJIUzI1NiJ9.eyJ0aWQiOjE2MDMzOTMyOSwidWlkIjozMDQyMzIzOSwiaWFkIjoiMjAyMi0wNS0xM1QwMTo0NDoxOS41ODFaIiwicGVyIjoibWU6d3JpdGUiLCJhY3RpZCI6MTIxMzI5MzYsInJnbiI6InVzZTEifQ.Qgo9TUCwGHEh2COUjuIkwGIajJjURSy0IgaSt8xH_T8"""
apiUrl = "https://api.monday.com/v2"
headers = {"Authorization": apiKey}
monday = MondayClient(apiKey)


def parse_args():
    """
    Argparser.
    """
    parser = ArgumentParser(description="Create items and upload files through Monday API.")

    parser.add_argument(
        "--path", type=str, help="Path to file to upload.", required=True
    )
    parser.add_argument(
        "--file", type=str, help="Name of file to upload.", required=True
    )
    parser.add_argument(
        "--board_id", type=int, help="Board id for item", required=True
    )
    parser.add_argument(
        "--group_id", type=str, help="Board id for item", required=True
    )
    return parser.parse_args()

def main():
    """
    Main function.
    """
    args = parse_args()
    res = monday.items.create_item(board_id=args.board_id, group_id=args.group_id, item_name=args.file)
    item_id = res['data']['create_item']['id']
    monday.items.add_file_to_column(item_id=item_id, column_id="files", file=args.path)

if __name__ == "__main__":
    main()
