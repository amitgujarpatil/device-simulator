const crypto = require("crypto");
const debug = require("debug");
const log = debug("EncryptionHandler log");
const errorLog = debug("EncryptionHandler error");

/**
 * Encryption Handler for MQTT Broker
 * Implements hybrid envelope encryption/decryption using AES-ECB + RSA-2048
 *
 * Packet Format:
 * - Bytes 0-1: Key version (16-bit big-endian)
 * - Bytes 2-257: RSA-encrypted AES key (256 bytes)
 * - Bytes 258-261: Data Length (32-bit big-endian)
 * - Bytes 262 to 262+DataLength-1: AES-ECB encrypted payload (variable length JSON)
 * - Remaining bytes: Unencrypted JSON string (contains GPS date, time, network time, etc.)
 */

// AES-ECB config per key version. To add a new version append one entry here.
const VERSION_AES_CONFIG = {
    1: { keySize: 16, algorithm: "aes-128-ecb" },
    2: { keySize: 32, algorithm: "aes-256-ecb" },
};
const DEFAULT_AES_CONFIG = VERSION_AES_CONFIG[1];

class EncryptionHandler {
    constructor(keyManager) {
        this.keyManager = keyManager;
        this.ENCRYPTED_PACKET_MIN_SIZE = 262; // 2 bytes version + 256 bytes encrypted AES key + 4 bytes data length
        this.ENCRYPTED_AES_KEY_SIZE = 256;
        this.VERSION_HEADER_SIZE = 2;
        this.DATA_LENGTH_SIZE = 4; // 32-bit big-endian for data length
    }

    _getAesConfig(version) {
        return VERSION_AES_CONFIG[version] || DEFAULT_AES_CONFIG;
    }

    /**
     * Detect if a packet is encrypted based on packet structure
     * @async
     * @param {Buffer} packet
     * @returns {Promise<boolean>}
     */
    async isEncryptedPacket(packet) {
        if (!Buffer.isBuffer(packet)) {
            return false;
        }

        // Must be at least minimum encrypted packet size
        if (packet.length < this.ENCRYPTED_PACKET_MIN_SIZE) {
            return false;
        }

        // Read version from first 2 bytes
        const version = packet.readUInt16BE(0);
        
        // Version should be a reasonable number (1-1000)
        // This helps distinguish encrypted packets from regular data
        if (version < 1 || version > 1000) {
            return false;
        }

        // Check if this version exists in key manager
        const isValid = await this.keyManager.isKeyValid(version);
        if (!isValid) {
            // If version is valid number but key doesn't exist, 
            // it might still be encrypted but we can't decrypt
            log("[EncryptionHandler] Packet appears encrypted (version: " + version + ") but key not found");
            return false;
        }

        return true;
    }

    /**
     * Decrypt an encrypted packet
     * @async
     * @param {Buffer} packet - Encrypted packet
     * @returns {Promise<Buffer>} Merged JSON as Buffer from decrypted payload and trailing JSON
     * @throws {Error} If decryption fails
     */
    async decryptPacket(packet) {
        if (!Buffer.isBuffer(packet)) {
            throw new Error("Packet must be a Buffer");
        }

        if (packet.length < this.ENCRYPTED_PACKET_MIN_SIZE) {
            throw new Error("Packet too small - minimum " + this.ENCRYPTED_PACKET_MIN_SIZE + " bytes required");
        }

        try {
            // Parse packet header
            const version = packet.readUInt16BE(0);  // 2 bytes
            const encryptedAesKey = packet.slice(this.VERSION_HEADER_SIZE, this.VERSION_HEADER_SIZE + this.ENCRYPTED_AES_KEY_SIZE);  // 256 bytes
            
            // Extract data length (32-bit big-endian)
            const dataLengthStart = this.VERSION_HEADER_SIZE + this.ENCRYPTED_AES_KEY_SIZE;
            const dataLength = packet.readUInt32BE(dataLengthStart);
            
            if (dataLength < 0) {
                throw new Error("Invalid data length: " + dataLength);
            }
            
            // Extract encrypted payload
            const encryptedPayloadStart = dataLengthStart + this.DATA_LENGTH_SIZE;
            const encryptedPayload = packet.slice(encryptedPayloadStart, encryptedPayloadStart + dataLength);
            
            // Extract trailing JSON string (unencrypted)
            const trailingJsonStart = encryptedPayloadStart + dataLength;
            const trailingJsonBuffer = packet.slice(trailingJsonStart);

            log("[EncryptionHandler] Decrypting packet:");
            log("  - Key version: " + version);
            log("  - Encrypted AES key size: " + encryptedAesKey.length + " bytes");
            log("  - Data length: " + dataLength + " bytes");
            log("  - Encrypted payload size: " + encryptedPayload.length + " bytes");
            log("  - Trailing JSON size: " + trailingJsonBuffer.length + " bytes");

            // Validate key version
            const isValid = await this.keyManager.isKeyValid(version);
            if (!isValid) {
                throw new Error("Key version " + version + " is not valid or not found");
            }

            // Get private key for decryption
            const privateKey = await this.keyManager.getPrivateKey(version);
            log("[EncryptionHandler] Retrieved RSA private key for version " + version);

            // Decrypt AES key using RSA private key with OAEP/SHA-1 padding
            const aesKey = crypto.privateDecrypt(
                {
                    key: privateKey,
                    padding: crypto.constants.RSA_PKCS1_OAEP_PADDING,
                    oaepHash: "sha1"
                },
                encryptedAesKey
            );

            log("[EncryptionHandler] Decrypted AES key: " + aesKey.length + " bytes");

            // Get AES config for this version
            const aesConfig = this._getAesConfig(version);

            // Validate AES key size for this version
            if (aesKey.length !== aesConfig.keySize) {
                throw new Error("Invalid AES key length: " + aesKey.length + " (expected " + aesConfig.keySize + " bytes for version " + version + ")");
            }

            // Decrypt payload with version-specific AES-ECB algorithm
            const decipher = crypto.createDecipheriv(aesConfig.algorithm, aesKey, null);
            decipher.setAutoPadding(false); // no padding removal, matches openssl -nopad
            
            let decryptedPayload;
            try {
                decryptedPayload = Buffer.concat([
                    decipher.update(encryptedPayload),
                    decipher.final()
                ]);
            } catch (err) {
                if (typeof decipher.destroy === "function") decipher.destroy();
                throw err;
            }

            log("[EncryptionHandler] Successfully decrypted payload: " + decryptedPayload.length + " bytes");
            
            // Parse decrypted payload as JSON (strip trailing null bytes from block-padding)
            let decryptedJson = {};
            try {
                let rawString = decryptedPayload.toString("utf8");
                // Trim whitespace and remove common non-printable padding characters (0x00 - 0x1F)
                // eslint-disable-next-line no-control-regex
                const cleanPayload = rawString.replace(/[\x00-\x1F]+$/, "").trim();
                decryptedJson = JSON.parse(cleanPayload);
                log("[EncryptionHandler] Parsed decrypted JSON successfully");
            } catch (err) {
                errorLog("[EncryptionHandler] Failed to parse decrypted payload as JSON:", err.message);
                throw new Error("Decrypted payload is not valid JSON: " + err.message);
            }
            
            // Parse trailing JSON string
            let trailingJson = {};
            if (trailingJsonBuffer.length > 0) {
                try {
                    // Strip trailing null bytes from block-padding before parsing using .replace(/\0+$/, "")
                    const rawString = trailingJsonBuffer.toString("utf8").replace(/\0+$/, "");
                    
                    // Find the first '{' and last '}' to extract valid JSON
                    const firstBrace = rawString.indexOf("{");
                    const lastBrace = rawString.lastIndexOf("}");
                    
                    if (firstBrace !== -1 && lastBrace !== -1 && lastBrace > firstBrace) {
                        const jsonString = rawString.substring(firstBrace, lastBrace + 1);
                        trailingJson = JSON.parse(jsonString);
                        log("[EncryptionHandler] Parsed trailing JSON successfully");
                    } else {
                        errorLog("[EncryptionHandler] No valid JSON structure found in trailing data");
                    }
                } catch (err) {
                    errorLog("[EncryptionHandler] Failed to parse trailing JSON:", err.message);
                    // Don't throw - trailing JSON might be optional
                }
            }
            
            // Merge both JSON objects (trailing JSON overwrites decrypted JSON if keys conflict)
            const mergedJson = Object.assign({}, decryptedJson, trailingJson);
            log("[EncryptionHandler] Merged JSON objects");
            
            // Convert merged JSON to Buffer
            const mergedJsonString = JSON.stringify(mergedJson);
            const mergedBuffer = Buffer.from(mergedJsonString, "utf8");
            log("[EncryptionHandler] Converted merged JSON to Buffer: " + mergedBuffer.length + " bytes");
            
            return mergedBuffer;

        } catch (err) {
            errorLog("[EncryptionHandler] Decryption failed:", err.message);
            throw new Error("Failed to decrypt packet: " + err.message);
        }
    }

    /**
     * Process a packet - decrypt if encrypted, otherwise return as-is
     * This ensures backward compatibility with non-encrypted packets
     * 
     * @async
     * @param {Buffer} packet
     * @returns {Promise<Object>} { decrypted: boolean, payload: Buffer|Object }
     */
    async processPacket(packet) {
        if (!Buffer.isBuffer(packet)) {
            // If not a buffer, return as-is
            return {
                decrypted: false,
                payload: packet
            };
        }

        // Check if packet is encrypted
        const isEncrypted = await this.isEncryptedPacket(packet);
        if (isEncrypted) {
            try {
                const decryptedBuffer = await this.decryptPacket(packet);
                log("[EncryptionHandler] Packet was encrypted and successfully decrypted");
                
                return {
                    decrypted: true,
                    payload: decryptedBuffer
                };
            } catch (err) {
                errorLog("[EncryptionHandler] Failed to decrypt packet that appeared encrypted:", err.message);
                // If decryption fails, treat as non-encrypted packet
                // This provides additional backward compatibility
                return {
                    decrypted: false,
                    payload: packet,
                    error: err.message
                };
            }
        } else {
            // Non-encrypted packet, return as-is
            return {
                decrypted: false,
                payload: packet
            };
        }
    }

    /**
     * Get encryption statistics
     * @async
     * @returns {Promise<Object>}
     */
    async getStats() {
        const currentVersion = this.keyManager.getCurrentVersion();
        return {
            minPacketSize: this.ENCRYPTED_PACKET_MIN_SIZE,
            rsaEncryptedKeySize: this.ENCRYPTED_AES_KEY_SIZE,
            dataLengthSize: this.DATA_LENGTH_SIZE,
            currentKeyVersion: currentVersion,
            currentAesConfig: this._getAesConfig(currentVersion),
            availableKeyVersions: await this.keyManager.listVersions()
        };
    }
}

module.exports = EncryptionHandler;