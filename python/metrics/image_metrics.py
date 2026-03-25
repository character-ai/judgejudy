#!/usr/bin/env python3
"""Image metrics: fid, lpips, clip_score, ssim."""

import argparse
import json
import sys


def compute_fid(data: dict) -> dict:
    try:
        from cleanfid import fid as cleanfid
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "clean-fid package not installed. Run: pip install clean-fid",
        }

    generated_dir = data.get("generated_dir")
    reference_dir = data.get("reference_dir")
    if not generated_dir or not reference_dir:
        return {
            "score": None,
            "raw": None,
            "error": "fid metric requires 'generated_dir' and 'reference_dir' fields",
        }

    params = data.get("params", {})
    mode = params.get("mode", "clean")

    fid_value = cleanfid.compute_fid(generated_dir, reference_dir, mode=mode)
    normalized = 1.0 / (1.0 + fid_value)

    return {
        "score": normalized,
        "raw": {"fid": fid_value, "normalized": normalized},
        "error": None,
    }


def compute_lpips(data: dict) -> dict:
    try:
        import lpips as lpips_lib
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "lpips package not installed. Run: pip install lpips",
        }

    try:
        import torch
        from torchvision import transforms
        from PIL import Image
    except ImportError as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Missing dependency: {e}. Run: pip install torch torchvision Pillow",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    reference_path = data.get("reference_path") or data.get("reference_file_path")
    if not generated_path or not reference_path:
        return {
            "score": None,
            "raw": None,
            "error": "lpips metric requires 'generated_path' and 'reference_path' fields",
        }

    params = data.get("params", {})
    net = params.get("net", "alex")

    loss_fn = lpips_lib.LPIPS(net=net)

    transform = transforms.Compose([
        transforms.Resize((256, 256)),
        transforms.ToTensor(),
        transforms.Normalize(mean=[0.5, 0.5, 0.5], std=[0.5, 0.5, 0.5]),
    ])

    img_gen = transform(Image.open(generated_path).convert("RGB")).unsqueeze(0)
    img_ref = transform(Image.open(reference_path).convert("RGB")).unsqueeze(0)

    with torch.no_grad():
        lpips_value = loss_fn(img_gen, img_ref).item()

    normalized = 1.0 - lpips_value

    return {
        "score": normalized,
        "raw": {"lpips": lpips_value, "normalized": normalized},
        "error": None,
    }


def compute_clip_score(data: dict) -> dict:
    try:
        import torch
        from PIL import Image
    except ImportError as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Missing dependency: {e}. Run: pip install torch Pillow",
        }

    try:
        from transformers import CLIPProcessor, CLIPModel
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "transformers package not installed. Run: pip install transformers",
        }

    image_path = data.get("image_path") or data.get("file_path")
    prompt = data.get("prompt") or data.get("input")
    if not image_path or not prompt:
        return {
            "score": None,
            "raw": None,
            "error": "clip_score metric requires 'image_path' and 'prompt' fields",
        }

    params = data.get("params", {})
    model_name = params.get("model", "openai/clip-vit-base-patch32")

    model = CLIPModel.from_pretrained(model_name)
    processor = CLIPProcessor.from_pretrained(model_name)

    image = Image.open(image_path).convert("RGB")
    inputs = processor(text=[prompt], images=image, return_tensors="pt", padding=True)

    with torch.no_grad():
        outputs = model(**inputs)
        image_embeds = outputs.image_embeds
        text_embeds = outputs.text_embeds

        image_embeds = image_embeds / image_embeds.norm(p=2, dim=-1, keepdim=True)
        text_embeds = text_embeds / text_embeds.norm(p=2, dim=-1, keepdim=True)

        cosine_sim = torch.sum(image_embeds * text_embeds, dim=-1).item()

    # Cosine similarity is in [-1, 1], normalize to [0, 1]
    normalized = (cosine_sim + 1.0) / 2.0

    return {
        "score": normalized,
        "raw": {"cosine_similarity": cosine_sim, "normalized": normalized},
        "error": None,
    }


def compute_ssim(data: dict) -> dict:
    try:
        import torch
        from torchvision import transforms
        from PIL import Image
    except ImportError as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Missing dependency: {e}. Run: pip install torch torchvision Pillow",
        }

    try:
        from torchmetrics.image import StructuralSimilarityIndexMeasure
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "torchmetrics package not installed. Run: pip install torchmetrics",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    reference_path = data.get("reference_path") or data.get("reference_file_path")
    if not generated_path or not reference_path:
        return {
            "score": None,
            "raw": None,
            "error": "ssim metric requires 'generated_path' and 'reference_path' fields",
        }

    transform = transforms.Compose([
        transforms.Resize((256, 256)),
        transforms.ToTensor(),
    ])

    img_gen = transform(Image.open(generated_path).convert("RGB")).unsqueeze(0)
    img_ref = transform(Image.open(reference_path).convert("RGB")).unsqueeze(0)

    ssim_metric = StructuralSimilarityIndexMeasure(data_range=1.0)
    ssim_value = ssim_metric(img_gen, img_ref).item()

    return {
        "score": ssim_value,
        "raw": {"ssim": ssim_value},
        "error": None,
    }


METRICS = {
    "fid": compute_fid,
    "lpips": compute_lpips,
    "clip_score": compute_clip_score,
    "ssim": compute_ssim,
}


def main():
    parser = argparse.ArgumentParser(description="Compute image metrics")
    parser.add_argument("--input", required=True, help="Path to input JSON file")
    parser.add_argument("--output", required=True, help="Path to output JSON file")
    args = parser.parse_args()

    try:
        with open(args.input, "r") as f:
            data = json.load(f)

        metric = data.get("metric")
        if metric not in METRICS:
            result = {
                "score": None,
                "raw": None,
                "error": f"Unknown metric: {metric}. Supported: {list(METRICS.keys())}",
            }
        else:
            result = METRICS[metric](data)

        with open(args.output, "w") as f:
            json.dump(result, f, indent=2)

        if result.get("error"):
            sys.exit(1)
        sys.exit(0)

    except Exception as e:
        result = {"score": None, "raw": None, "error": str(e)}
        try:
            with open(args.output, "w") as f:
                json.dump(result, f, indent=2)
        except Exception:
            print(json.dumps(result), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
