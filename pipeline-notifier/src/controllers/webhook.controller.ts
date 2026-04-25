import { Router } from "express";
import { handleWebhook } from "../services/webhook.service";

const router = Router();

router.post("/github", async (req, res) => {
  try {
    await handleWebhook(req.body);
    res.sendStatus(200);
  } catch (err) {
    console.error(err);
    res.sendStatus(500);
  }
});

export default router;
