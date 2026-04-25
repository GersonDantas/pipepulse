import express from "express";
import bodyParser from "body-parser";
import webhookRoutes from "./controllers/webhook.controller";

const app = express();

app.use(bodyParser.json());

app.use("/webhook", webhookRoutes);

app.listen(3000, () => {
  console.log("🚀 Server running on port 3000");
});
