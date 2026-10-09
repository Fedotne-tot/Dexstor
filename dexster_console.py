#!/usr/bin/env python3

import tkinter as tk
from tkinter import scrolledtext
import subprocess
import base64
import os
import threading


WORKDIR = os.path.expanduser("~/DEXSTER/destor-merge")


def run_command(source_text, decode_b64=False):
    output.delete("1.0", tk.END)

    def worker():
        try:
            command_text = source_text.strip()

            if decode_b64:
                command_text = base64.b64decode(
                    command_text
                ).decode("utf-8")

            result = subprocess.run(
                command_text,
                shell=True,
                cwd=WORKDIR,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                executable="/bin/bash",
            )

            text = (
                "$ " + command_text +
                "\n\n" +
                result.stdout +
                "\n[exit code: " +
                str(result.returncode) +
                "]"
            )

            root.after(
                0,
                lambda: (
                    output.insert(tk.END, text),
                    output.see(tk.END),
                ),
            )

        except Exception as e:
            root.after(
                0,
                lambda: output.insert(
                    tk.END,
                    f"ERROR:\n{e}"
                ),
            )

    threading.Thread(
        target=worker,
        daemon=True,
    ).start()


def normal_run():
    run_command(
        command.get("1.0", tk.END)
    )


def b64_run():
    run_command(
        command.get("1.0", tk.END),
        True,
    )


root = tk.Tk()
root.title("DEXSTER Console")
root.geometry("1000x700")

tk.Label(
    root,
    text="Команда для ~/DEXSTER/destor-merge",
    font=("Arial", 12),
).pack(pady=(8, 2))

command = scrolledtext.ScrolledText(
    root,
    height=10,
    font=("monospace", 11),
)
command.pack(
    fill=tk.BOTH,
    padx=10,
    pady=5,
)

frame = tk.Frame(root)
frame.pack(pady=5)

tk.Button(
    frame,
    text="▶ Выполнить",
    width=20,
    command=normal_run,
).pack(side=tk.LEFT, padx=5)

tk.Button(
    frame,
    text="▶ Base64 выполнить",
    width=20,
    command=b64_run,
).pack(side=tk.LEFT, padx=5)

tk.Button(
    frame,
    text="Очистить",
    width=15,
    command=lambda: command.delete(
        "1.0",
        tk.END,
    ),
).pack(side=tk.LEFT, padx=5)

tk.Label(
    root,
    text="Вывод",
    font=("Arial", 12),
).pack(pady=(5, 2))

output = scrolledtext.ScrolledText(
    root,
    height=20,
    font=("monospace", 10),
)
output.pack(
    fill=tk.BOTH,
    expand=True,
    padx=10,
    pady=5,
)

root.mainloop()
