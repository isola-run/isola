"use strict";
/**
 * Custom wrapper layer extending the generated Isola client with convenience methods.
 */
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](e)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { default: mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.IsolaClient = void 0;
const fs_1 = require("fs/promises");
const path_1 = __importDefault(require("path"));
const Client_js_1 = require("./Client.js");
class IsolaClient extends Client_js_1.IsolaClient {
    /**
     * Upload a file from the local filesystem to a sandbox.
     *
     * @param sandboxId - Target sandbox ID
     * @param localPath - Path to file on local filesystem
     * @param remotePath - Destination path in sandbox (defaults to filename in /home/user/)
     * @param requestOptions - Request-specific configuration
     * @returns Promise resolving to the upload response
     * @throws FileNotFoundError if the local file doesn't exist
     */
    uploadFileFromPath(sandboxId, localPath, remotePath, requestOptions) {
        return __awaiter(this, void 0, void 0, function* () {
            const content = yield (0, fs_1.readFile)(localPath);
            const fileName = path_1.default.basename(localPath);
            const targetPath = remotePath !== null && remotePath !== void 0 ? remotePath : `/home/user/${fileName}`;
            const response = yield this.files.uploadFile(sandboxId, {
                file: content,
                path: targetPath,
            }, requestOptions);
            return response.data;
        });
    }
}
exports.IsolaClient = IsolaClient;
