#!/usr/bin/env python3
"""Video metrics: temporal_consistency, clip_temporal, frame_quality."""

import argparse
import json
import sys


def _extract_frames(video_path: str, max_frames: int = 0):
    """Extract frames from video using OpenCV. Returns list of BGR numpy arrays."""
    try:
        import cv2
    except ImportError:
        raise ImportError(
            "opencv-python package not installed. Run: pip install opencv-python"
        )

    cap = cv2.VideoCapture(video_path)
    if not cap.isOpened():
        raise ValueError(f"Cannot open video: {video_path}")

    frames = []
    total_frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))

    if max_frames > 0 and total_frames > max_frames:
        # Sample frames uniformly
        indices = [int(i * total_frames / max_frames) for i in range(max_frames)]
        for idx in indices:
            cap.set(cv2.CAP_PROP_POS_FRAMES, idx)
            ret, frame = cap.read()
            if ret:
                frames.append(frame)
    else:
        while True:
            ret, frame = cap.read()
            if not ret:
                break
            frames.append(frame)

    cap.release()
    return frames


def compute_temporal_consistency(data: dict) -> dict:
    try:
        import cv2
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "opencv-python package not installed. Run: pip install opencv-python",
        }

    try:
        import numpy as np
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "numpy package not installed. Run: pip install numpy",
        }

    try:
        from torchmetrics.image import StructuralSimilarityIndexMeasure
        import torch
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "torchmetrics/torch not installed. Run: pip install torchmetrics torch",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    if not generated_path:
        return {
            "score": None,
            "raw": None,
            "error": "temporal_consistency metric requires 'generated_path' field",
        }

    params = data.get("params", {})
    max_frames = params.get("max_frames", 0)

    try:
        frames = _extract_frames(generated_path, max_frames)
    except (ImportError, ValueError) as e:
        return {"score": None, "raw": None, "error": str(e)}

    if len(frames) < 2:
        return {
            "score": None,
            "raw": None,
            "error": "Video has fewer than 2 frames",
        }

    ssim_metric = StructuralSimilarityIndexMeasure(data_range=1.0)
    ssim_scores = []

    for i in range(len(frames) - 1):
        frame_a = torch.from_numpy(frames[i]).permute(2, 0, 1).unsqueeze(0).float() / 255.0
        frame_b = torch.from_numpy(frames[i + 1]).permute(2, 0, 1).unsqueeze(0).float() / 255.0

        # Resize to common size for SSIM
        target_h, target_w = 256, 256
        frame_a = torch.nn.functional.interpolate(frame_a, size=(target_h, target_w), mode="bilinear")
        frame_b = torch.nn.functional.interpolate(frame_b, size=(target_h, target_w), mode="bilinear")

        ssim_val = ssim_metric(frame_a, frame_b).item()
        ssim_scores.append(ssim_val)

    mean_ssim = float(np.mean(ssim_scores))

    return {
        "score": mean_ssim,
        "raw": {
            "mean_ssim": mean_ssim,
            "num_frames": len(frames),
            "num_pairs": len(ssim_scores),
            "min_ssim": float(np.min(ssim_scores)),
            "max_ssim": float(np.max(ssim_scores)),
        },
        "error": None,
    }


def compute_clip_temporal(data: dict) -> dict:
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

    try:
        import numpy as np
        import cv2
    except ImportError as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Missing dependency: {e}",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    if not generated_path:
        return {
            "score": None,
            "raw": None,
            "error": "clip_temporal metric requires 'generated_path' field",
        }

    params = data.get("params", {})
    num_frames = params.get("num_frames", 16)
    model_name = params.get("model", "openai/clip-vit-base-patch32")

    try:
        frames = _extract_frames(generated_path, num_frames)
    except (ImportError, ValueError) as e:
        return {"score": None, "raw": None, "error": str(e)}

    if len(frames) < 2:
        return {
            "score": None,
            "raw": None,
            "error": "Video has fewer than 2 frames",
        }

    model = CLIPModel.from_pretrained(model_name)
    processor = CLIPProcessor.from_pretrained(model_name)

    embeddings = []
    for frame in frames:
        # Convert BGR to RGB PIL Image
        rgb = cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)
        pil_img = Image.fromarray(rgb)
        inputs = processor(images=pil_img, return_tensors="pt")
        with torch.no_grad():
            emb = model.get_image_features(**inputs)
            if not isinstance(emb, torch.Tensor):
                emb = emb.pooler_output if hasattr(emb, 'pooler_output') else emb[0]
            emb = emb / emb.norm(p=2, dim=-1, keepdim=True)
            embeddings.append(emb)

    cosine_sims = []
    for i in range(len(embeddings) - 1):
        sim = torch.sum(embeddings[i] * embeddings[i + 1], dim=-1).item()
        cosine_sims.append(sim)

    mean_sim = float(np.mean(cosine_sims))
    # Normalize from [-1, 1] to [0, 1]
    normalized = (mean_sim + 1.0) / 2.0

    return {
        "score": normalized,
        "raw": {
            "mean_cosine_similarity": mean_sim,
            "normalized": normalized,
            "num_frames": len(frames),
            "num_pairs": len(cosine_sims),
        },
        "error": None,
    }


def compute_frame_quality(data: dict) -> dict:
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
        import lpips as lpips_lib
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "lpips package not installed. Run: pip install lpips",
        }

    try:
        from torchmetrics.image import StructuralSimilarityIndexMeasure
    except ImportError:
        return {
            "score": None,
            "raw": None,
            "error": "torchmetrics package not installed. Run: pip install torchmetrics",
        }

    try:
        import numpy as np
        import cv2
    except ImportError as e:
        return {
            "score": None,
            "raw": None,
            "error": f"Missing dependency: {e}",
        }

    generated_path = data.get("generated_path") or data.get("file_path")
    reference_path = data.get("reference_path") or data.get("reference_file_path")
    if not generated_path or not reference_path:
        return {
            "score": None,
            "raw": None,
            "error": "frame_quality metric requires 'generated_path' and 'reference_path' fields",
        }

    params = data.get("params", {})
    max_frames = params.get("max_frames", 32)

    try:
        gen_frames = _extract_frames(generated_path, max_frames)
        ref_frames = _extract_frames(reference_path, max_frames)
    except (ImportError, ValueError) as e:
        return {"score": None, "raw": None, "error": str(e)}

    num_frames = min(len(gen_frames), len(ref_frames))
    if num_frames == 0:
        return {
            "score": None,
            "raw": None,
            "error": "No frames could be extracted",
        }

    target_h, target_w = 256, 256

    lpips_transform = transforms.Compose([
        transforms.Resize((target_h, target_w)),
        transforms.ToTensor(),
        transforms.Normalize(mean=[0.5, 0.5, 0.5], std=[0.5, 0.5, 0.5]),
    ])
    ssim_transform = transforms.Compose([
        transforms.Resize((target_h, target_w)),
        transforms.ToTensor(),
    ])

    loss_fn = lpips_lib.LPIPS(net="alex")
    ssim_metric = StructuralSimilarityIndexMeasure(data_range=1.0)

    lpips_scores = []
    ssim_scores = []

    for i in range(num_frames):
        gen_rgb = cv2.cvtColor(gen_frames[i], cv2.COLOR_BGR2RGB)
        ref_rgb = cv2.cvtColor(ref_frames[i], cv2.COLOR_BGR2RGB)
        gen_pil = Image.fromarray(gen_rgb)
        ref_pil = Image.fromarray(ref_rgb)

        # LPIPS
        gen_t = lpips_transform(gen_pil).unsqueeze(0)
        ref_t = lpips_transform(ref_pil).unsqueeze(0)
        with torch.no_grad():
            lpips_val = loss_fn(gen_t, ref_t).item()
        lpips_scores.append(lpips_val)

        # SSIM
        gen_s = ssim_transform(gen_pil).unsqueeze(0)
        ref_s = ssim_transform(ref_pil).unsqueeze(0)
        ssim_val = ssim_metric(gen_s, ref_s).item()
        ssim_scores.append(ssim_val)

    mean_lpips = float(np.mean(lpips_scores))
    mean_ssim = float(np.mean(ssim_scores))

    # Combine: average of (1-LPIPS) and SSIM
    combined = (((1.0 - mean_lpips) + mean_ssim) / 2.0)

    return {
        "score": combined,
        "raw": {
            "mean_lpips": mean_lpips,
            "mean_ssim": mean_ssim,
            "combined": combined,
            "num_frames_compared": num_frames,
        },
        "error": None,
    }


METRICS = {
    "temporal_consistency": compute_temporal_consistency,
    "clip_temporal": compute_clip_temporal,
    "frame_quality": compute_frame_quality,
}


def main():
    parser = argparse.ArgumentParser(description="Compute video metrics")
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
