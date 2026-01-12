/**
 * Custom wrapper layer extending the generated Isola client with convenience methods.
 */

import * as fs from "fs/promises";
import * as path from "path";
import { IsolaClient as _IsolaClient } from "./Client.mjs";
import * as Isola from "./api/index.mjs";

export class IsolaClient extends _IsolaClient {
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
  async uploadFileFromPath(
    sandboxId: string,
    localPath: string,
    remotePath?: string,
    requestOptions?: IsolaClient.RequestOptions,
  ): Promise<Isola.FileUploadResponse> {
    const content = await fs.readFile(localPath);
    const fileName = path.basename(localPath);
    const targetPath = remotePath ?? `/home/user/${fileName}`;

    const response = await this.files.uploadFile(
      sandboxId,
      {
        file: content,
        path: targetPath,
      },
      requestOptions,
    );

    return response.data;
  }
}
